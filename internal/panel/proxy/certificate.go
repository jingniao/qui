package proxy

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

// CertificateInfo is the read-only view of the certificate the panel stores for
// an inbound (v1.4.7 section 3). It answers the two questions the editor has to
// show: what the certificate covers, and when it expires. Private key material is
// never part of this view.
type CertificateInfo struct {
	Subject         string   `json:"subject"`
	DNSNames        []string `json:"dnsNames"`
	IPAddresses     []string `json:"ipAddresses"`
	NotAfter        string   `json:"notAfter"`
	SelfSigned      bool     `json:"selfSigned"`
	CoverageWarning string   `json:"coverageWarning"`
}

// certificateInfo describes the certificate inside one settings block. Only
// sources the panel holds itself can be described: a node-side file is unreadable
// here, so it returns nil and the editor shows no local details for it.
func certificateInfo(settings desiredstate.ProtocolSettings, source, connectAddress string) (*CertificateInfo, error) {
	if source == TLSSourceFile || settings.CertificatePEM == "" {
		return nil, nil
	}
	block, _ := pem.Decode([]byte(settings.CertificatePEM))
	if block == nil {
		return nil, errors.New("stored TLS certificate is not PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	info := &CertificateInfo{
		Subject:     leaf.Subject.String(),
		DNSNames:    append([]string{}, leaf.DNSNames...),
		IPAddresses: []string{},
		NotAfter:    leaf.NotAfter.UTC().Format(time.RFC3339),
		SelfSigned:  bytes.Equal(leaf.RawIssuer, leaf.RawSubject) && leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) == nil,
	}
	for _, address := range leaf.IPAddresses {
		info.IPAddresses = append(info.IPAddresses, address.String())
	}
	info.CoverageWarning = coverageWarning(leaf, settings.ServerName, connectAddress)
	return info, nil
}

// coverageWarning reports whether a client would reject this certificate. The
// names it has to fit are the server name clients verify and the address they
// dial; x509.VerifyHostname does the matching (RFC 6125 wildcards and IP
// addresses), so the panel never reimplements it. An empty list of names needs no
// warning: there is nothing to be wrong about.
func coverageWarning(leaf *x509.Certificate, serverName, connectAddress string) string {
	var names []string
	for _, name := range []string{serverName, connectAddress} {
		if name == "" {
			continue
		}
		if leaf.VerifyHostname(name) == nil {
			return ""
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("证书不覆盖 %s，客户端会校验失败：请更换或重新生成覆盖该地址的证书。", strings.Join(names, "、"))
}

// CertificateStore is the certificate library an acme inbound reads from. The
// proxy module owns no certificate data: an inbound only references a row, and
// the PEM pair is read when the inbound is loaded, so a renewal needs no inbound
// write. exists is false for an unknown ID, and an empty certPEM means nothing
// has been issued yet.
type CertificateStore interface {
	CertificateMaterial(ctx context.Context, certificateID string) (certPEM, keyPEM string, exists bool, err error)
}

// injectCertificate puts the stored pair of an acme inbound into its settings. An
// unavailable pair is left empty instead of failing the read: listing and editing
// must keep working, and the snapshot refuses to publish such an inbound.
func (r *Repository) injectCertificate(ctx context.Context, inbound *Inbound) error {
	if inbound.TLSSource != TLSSourceACME || inbound.CertificateID == "" || r.Certificates == nil {
		return nil
	}
	certPEM, keyPEM, exists, err := r.Certificates.CertificateMaterial(ctx, inbound.CertificateID)
	if err != nil || !exists {
		return err
	}
	inbound.Settings.CertificatePEM, inbound.Settings.PrivateKeyPEM = certPEM, keyPEM
	return nil
}

// storedSettings is what the sealed column keeps. An acme inbound holds no copy
// of its certificate: the certificate library is the only source of the pair, so
// a renewal can never leave a stale PEM behind in the inbound.
func storedSettings(input InboundInput) desiredstate.ProtocolSettings {
	settings := input.Settings
	if input.TLSSource == TLSSourceACME {
		settings.CertificatePEM, settings.PrivateKeyPEM, settings.TrustedCAPEM = "", "", ""
	}
	return settings
}

// useStoredCertificate resolves the certificate an acme inbound references and
// fills the settings the way publication and validation see them. Any other
// source drops the reference, so a certificate is only ever "in use" by an
// inbound that really presents it.
func (r *Repository) useStoredCertificate(ctx context.Context, input *InboundInput, pastedKey bool) error {
	if input.TLSSource != TLSSourceACME {
		input.CertificateID = ""
		return nil
	}
	if pastedKey {
		return errors.New("自动申请证书不能同时粘贴证书私钥")
	}
	if input.CertificateID == "" {
		return errors.New("请选择要使用的证书")
	}
	if r.Certificates == nil {
		return errors.New("证书库不可用")
	}
	certPEM, keyPEM, exists, err := r.Certificates.CertificateMaterial(ctx, input.CertificateID)
	switch {
	case err != nil:
		return err
	case !exists:
		return errors.New("所选证书不存在")
	case certPEM == "" || keyPEM == "":
		return errors.New("所选证书尚未签发成功")
	}
	input.Settings.CertificatePEM, input.Settings.PrivateKeyPEM, input.Settings.TrustedCAPEM = certPEM, keyPEM, ""
	if input.Settings.ServerName == "" {
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			return errors.New("所选证书无法解析")
		}
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return errors.New("所选证书无法解析")
		}
		for _, name := range leaf.DNSNames {
			if !strings.HasPrefix(name, "*.") {
				input.Settings.ServerName = name
				break
			}
		}
	}
	return nil
}

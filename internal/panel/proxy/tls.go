package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"proxycontrol/pkg/contracts/desiredstate"
	"time"
)

// A certificate the panel generates itself, in the two lifetimes this panel
// needs. An internal relay's material is written by the panel and replaced with
// the relay, so a year is plenty. A user inbound's certificate is pinned by the
// clients through their subscription, so it must outlive the subscription: ten
// years keeps a pinned client working until the Owner rotates it deliberately.
const (
	relayCertificateValidity = 365 * 24 * time.Hour
	userCertificateValidity  = 10 * 365 * 24 * time.Hour
)

// managedTLS issues one self-signed certificate for serverName and returns the
// settings that present it. CertificatePEM and PrivateKeyPEM are the pair the
// core renders, TrustedCAPEM is the same certificate so a subscription can pin
// it (AGENTS.md section 10.3), and ServerName is the name clients verify.
func managedTLS(serverName string, validity time.Duration) (desiredstate.ProtocolSettings, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return desiredstate.ProtocolSettings{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return desiredstate.ProtocolSettings{}, err
	}
	now := time.Now().UTC()
	certificate := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: serverName}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(validity), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if ip := net.ParseIP(serverName); ip != nil {
		certificate.IPAddresses = []net.IP{ip}
	} else {
		certificate.DNSNames = []string{serverName}
	}
	encoded, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		return desiredstate.ProtocolSettings{}, err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return desiredstate.ProtocolSettings{}, err
	}
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: encoded}))
	return desiredstate.ProtocolSettings{Security: "tls", ServerName: serverName, CertificatePEM: cert, PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), TrustedCAPEM: cert}, nil
}

// The certificate sources an inbound can record (v1.4.7 section 3, phase C1).
// The column also allows the empty value, which means a row written before 0136
// or an inbound that presents no certificate at all.
const (
	TLSSourcePaste      = "paste"
	TLSSourceSelfSigned = "self_signed"
	TLSSourceFile       = "file"
	// TLSSourceACME references a certificate the panel obtained itself through
	// Cloudflare DNS-01 (v1.4.7 section 4). The inbound stores only the
	// certificate ID; the PEM pair is read from the certificate library.
	TLSSourceACME = "acme"
)

// validTLSSource accepts the recorded sources plus the empty legacy value.
func validTLSSource(value string) bool {
	switch value {
	case "", TLSSourcePaste, TLSSourceSelfSigned, TLSSourceFile, TLSSourceACME:
		return true
	}
	return false
}

// tlsSourceOf resolves the source recorded for an inbound. A row written before
// 0136 carries no source, and the certificate itself is inside the sealed
// settings column, so the migration cannot classify it: the only material such a
// row can hold is a pasted certificate, which is why a TLS inbound with one reads
// as paste here instead of as unknown.
func tlsSourceOf(stored string, settings desiredstate.ProtocolSettings) string {
	if stored != "" || settings.CertificatePEM == "" {
		return stored
	}
	return TLSSourcePaste
}

// validateCertificateSource keeps the recorded source and the material it names
// in step: a file source needs its two paths, every other source must not carry
// them, and a source only exists on a TLS listener.
func validateCertificateSource(input InboundInput) error {
	if input.TLSSource == "" {
		return nil
	}
	if input.Settings.Security != "tls" || input.Settings.Reality != nil {
		return errors.New("证书来源只能用于 TLS 入站")
	}
	switch input.TLSSource {
	case TLSSourceFile:
		if !input.Settings.HasCertificateFiles() {
			return errors.New("证书来源为节点文件时必须填写证书与私钥路径")
		}
	case TLSSourcePaste, TLSSourceSelfSigned, TLSSourceACME:
		if input.Settings.HasCertificateFiles() {
			return errors.New("证书来源不是节点文件时不能填写证书路径")
		}
	}
	return nil
}

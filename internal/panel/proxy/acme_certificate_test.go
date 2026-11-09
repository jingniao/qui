package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

// caIssuedPair is a leaf certificate signed by a throwaway CA, which is what a
// public CA delivers: it is not self-signed.
func caIssuedPair(t *testing.T, names ...string) (certPEM, keyPEM string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: names[0]}, DNSNames: names, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(90 * 24 * time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
}

// fakeCertificates is the certificate library: a row exists once added, and its
// material may be empty to stand for a request that has not been issued.
type fakeCertificates struct {
	material map[string][2]string
}

func (f *fakeCertificates) CertificateMaterial(_ context.Context, id string) (string, string, bool, error) {
	pair, ok := f.material[id]
	return pair[0], pair[1], ok, nil
}

// addCertificate registers a certificate row, which the inbound's foreign key
// needs, together with the library's view of its material.
func addCertificate(t *testing.T, f *fixture, library *fakeCertificates, id, certPEM, keyPEM string) {
	t.Helper()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := f.service.Repository.Database.Exec("INSERT INTO certificates(id,domains_json,status,auto_dns,created_at,updated_at) VALUES(?,?,?,0,?,?)", id, `["edge.example.com"]`, "valid", stamp, stamp); err != nil {
		t.Fatal(err)
	}
	library.material[id] = [2]string{certPEM, keyPEM}
}

func acmeInbound(nodeID, certificateID string, port int) InboundInput {
	return InboundInput{Name: "vless acme", NodeID: nodeID, Protocol: "vless", Listen: "0.0.0.0", Port: port, Enabled: true, DefaultOutbound: "direct", TLSSource: TLSSourceACME, CertificateID: certificateID, Settings: desiredstate.ProtocolSettings{Security: "tls"}}
}

func acmeFixture(t *testing.T) (*fixture, *fakeCertificates) {
	t.Helper()
	f := setup(t)
	library := &fakeCertificates{material: map[string][2]string{}}
	f.service.Repository.Certificates = library
	return f, library
}

func storedCertificateID(t *testing.T, f *fixture, inboundID string) sql.NullString {
	t.Helper()
	var id sql.NullString
	if err := f.service.Repository.Database.QueryRow("SELECT certificate_id FROM inbounds WHERE id=?", inboundID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestACMEInboundNeedsAnIssuedCertificate(t *testing.T) {
	f, library := acmeFixture(t)
	ctx := context.Background()
	certPEM, keyPEM := caIssuedPair(t, "*.example.com", "edge.example.com")
	addCertificate(t, f, library, "valid", certPEM, keyPEM)
	addCertificate(t, f, library, "pending", "", "")

	withFiles := acmeInbound(f.nodeA, "valid", 24443)
	withFiles.Settings.CertificatePath, withFiles.Settings.PrivateKeyPath = "/etc/proxy-agent/certs/a.pem", "/etc/proxy-agent/certs/a.key"
	pasted := acmeInbound(f.nodeA, "valid", 24443)
	pasted.Settings.PrivateKeyPEM = keyPEM
	relay := acmeInbound(f.nodeA, "valid", 24443)
	relay.InternalRelay = true
	plain := acmeInbound(f.nodeA, "valid", 24443)
	plain.Settings.Security = ""
	for name, input := range map[string]InboundInput{
		"missing": acmeInbound(f.nodeA, "unknown", 24443),
		"pending": acmeInbound(f.nodeA, "pending", 24443),
		"no ID":   acmeInbound(f.nodeA, "", 24443),
		"files":   withFiles,
		"pasted":  pasted,
		"relay":   relay,
		"no TLS":  plain,
	} {
		if _, err := f.service.SaveInbound(ctx, "", input); err == nil {
			t.Fatalf("%s: the inbound was saved", name)
		}
	}
	if _, err := f.service.SaveInbound(ctx, "", acmeInbound(f.nodeA, "pending", 24443)); err == nil || !strings.Contains(err.Error(), "尚未签发成功") {
		t.Fatalf("pending certificate: %v", err)
	}

	saved, err := f.service.SaveInbound(ctx, "", acmeInbound(f.nodeA, "valid", 24443))
	if err != nil {
		t.Fatal(err)
	}
	if saved.TLSSource != TLSSourceACME || saved.CertificateID != "valid" {
		t.Fatalf("saved: %+v", saved)
	}
	// The server name defaults to the first name that is not a wildcard.
	if revealed := revealInbound(t, f, saved.ID); revealed.Settings.ServerName != "edge.example.com" {
		t.Fatalf("server name: %q", revealed.Settings.ServerName)
	}
	if id := storedCertificateID(t, f, saved.ID); !id.Valid || id.String != "valid" {
		t.Fatalf("reference: %+v", id)
	}
}

// Leaving the acme source drops the reference and does not freeze the library's
// pair into the inbound as pasted material.
func TestLeavingACMEClearsTheCertificateReference(t *testing.T) {
	f, library := acmeFixture(t)
	ctx := context.Background()
	certPEM, keyPEM := caIssuedPair(t, "edge.example.com")
	addCertificate(t, f, library, "valid", certPEM, keyPEM)
	saved, err := f.service.SaveInbound(ctx, "", acmeInbound(f.nodeA, "valid", 24443))
	if err != nil {
		t.Fatal(err)
	}
	// A plain edit that names no source keeps both source and reference.
	plain := acmeInbound(f.nodeA, "", 24443)
	plain.TLSSource = ""
	if _, err := f.service.SaveInbound(ctx, saved.ID, plain); err != nil {
		t.Fatalf("plain edit: %v", err)
	}
	if id := storedCertificateID(t, f, saved.ID); id.String != "valid" {
		t.Fatalf("a plain edit lost the reference: %+v", id)
	}
	// Pasting without material fails instead of copying the library's pair.
	paste := acmeInbound(f.nodeA, "", 24443)
	paste.TLSSource = TLSSourcePaste
	if _, err := f.service.SaveInbound(ctx, saved.ID, paste); err == nil {
		t.Fatal("the library's pair became pasted material")
	}
	self := acmeInbound(f.nodeA, "valid", 24443)
	self.TLSSource = TLSSourceSelfSigned
	if _, err := f.service.SaveInbound(ctx, saved.ID, self); err != nil {
		t.Fatal(err)
	}
	if id := storedCertificateID(t, f, saved.ID); id.Valid {
		t.Fatalf("self-signed inbound still references a certificate: %+v", id)
	}
	if revealed := revealInbound(t, f, saved.ID); revealed.TLSSource != TLSSourceSelfSigned || revealed.Settings.CertificatePEM == certPEM {
		t.Fatalf("the inbound kept the library's certificate: %+v", revealed.TLSSource)
	}
	// The reference is also cleared for a source that never had one.
	other := selfSignedInbound(f.nodeB, 24443)
	other.CertificateID = "valid"
	created, err := f.service.SaveInbound(ctx, "", other)
	if err != nil {
		t.Fatal(err)
	}
	if id := storedCertificateID(t, f, created.ID); id.Valid {
		t.Fatalf("a self-signed create kept a reference: %+v", id)
	}
}

func TestACMEInboundIsRenderedFromTheLibraryAndStoresNoPEM(t *testing.T) {
	f, library := acmeFixture(t)
	ctx := context.Background()
	certPEM, keyPEM := caIssuedPair(t, "edge.example.com")
	addCertificate(t, f, library, "valid", certPEM, keyPEM)
	saved, err := f.service.SaveInbound(ctx, "", acmeInbound(f.nodeA, "valid", 24443))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.service.Repository.storedInbound(ctx, f.service.Repository.Database, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Settings.CertificatePEM != "" || stored.Settings.PrivateKeyPEM != "" || stored.Settings.TrustedCAPEM != "" {
		t.Fatal("the sealed settings of an acme inbound hold a PEM copy")
	}
	snapshot := func() (desiredstate.ProtocolSettings, error) {
		state, err := f.service.Snapshot(ctx, f.nodeA, f.users)
		if err != nil {
			return desiredstate.ProtocolSettings{}, err
		}
		for _, inbound := range state.Inbounds {
			if inbound.ID == saved.ID {
				return inbound.Settings, nil
			}
		}
		t.Fatal("inbound missing from the snapshot")
		return desiredstate.ProtocolSettings{}, nil
	}
	settings, err := snapshot()
	if err != nil || settings.CertificatePEM != certPEM || settings.PrivateKeyPEM != keyPEM {
		t.Fatalf("the snapshot did not carry the library's pair: %v", err)
	}
	// A renewal changes the library only; the next snapshot carries the new pair.
	renewedCert, renewedKey := caIssuedPair(t, "edge.example.com")
	library.material["valid"] = [2]string{renewedCert, renewedKey}
	if settings, err = snapshot(); err != nil || settings.CertificatePEM != renewedCert || settings.PrivateKeyPEM != renewedKey {
		t.Fatalf("the snapshot kept the old pair: %v", err)
	}
	// Without a pair the publication fails instead of reaching the node bare.
	library.material["valid"] = [2]string{}
	if _, err := snapshot(); err == nil || !strings.Contains(err.Error(), "尚未签发成功") {
		t.Fatalf("an inbound without its certificate was published: %v", err)
	}
}

func TestACMEInboundCertificateInfoAndMasking(t *testing.T) {
	f, library := acmeFixture(t)
	ctx := context.Background()
	certPEM, keyPEM := caIssuedPair(t, "edge.example.com", "*.cdn.example.com")
	addCertificate(t, f, library, "valid", certPEM, keyPEM)
	saved, err := f.service.SaveInbound(ctx, "", acmeInbound(f.nodeA, "valid", 24443))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Settings.PrivateKeyPEM != "" || !saved.HasTLSKey {
		t.Fatal("the private key is visible or not reported")
	}
	masked, err := f.service.Repository.Inbound(ctx, saved.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	info := masked.CertificateInfo
	if info == nil || info.SelfSigned || len(info.DNSNames) != 2 || info.DNSNames[0] != "edge.example.com" || info.NotAfter == "" || info.CoverageWarning != "" {
		t.Fatalf("certificate info: %+v", info)
	}
	if masked.Settings.PrivateKeyPEM != "" {
		t.Fatal("the masked view shows the private key")
	}
	// A server name the certificate does not cover is reported, as for any source.
	wrong := acmeInbound(f.nodeA, "valid", 24443)
	wrong.Settings.ServerName = "other.example.net"
	changed, err := f.service.SaveInbound(ctx, saved.ID, wrong)
	if err != nil {
		t.Fatal(err)
	}
	if changed.CertificateInfo == nil || !strings.Contains(changed.CertificateInfo.CoverageWarning, "other.example.net") {
		t.Fatalf("coverage warning: %+v", changed.CertificateInfo)
	}
}

package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthorityPersistsAndBindsNodeIdentity(t *testing.T) {
	directory := t.TempDir()
	authority, err := LoadOrCreate(directory)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadOrCreate(directory)
	if err != nil {
		t.Fatal(err)
	}
	if string(authority.PEM) != string(again.PEM) {
		t.Fatal("CA changed on restart")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "attacker-chosen-name"}, DNSNames: []string{"untrusted.example"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := ParseCSR(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	if err != nil {
		t.Fatal(err)
	}
	_, certificate, err := authority.IssueNode(csr, "expected-node")
	if err != nil {
		t.Fatal(err)
	}
	id, err := NodeID(certificate)
	if err != nil || id != "expected-node" || len(certificate.DNSNames) != 0 {
		t.Fatal("CSR injected certificate identity")
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(authority.PEM)
	if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "ca.crt")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(directory); err == nil {
		t.Fatal("replaced private key after incomplete CA")
	}
}

func TestExistingMaterialCheckNeverGeneratesKeys(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "missing-identity")
	if _, err := LoadExisting(directory); err == nil {
		t.Fatal("missing CA accepted")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("read-only CA check created directory", err)
	}
	authority, err := LoadOrCreate(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.ExistingDevelopmentServer(); err == nil {
		t.Fatal("missing server pair accepted")
	}
	if _, err := os.Stat(filepath.Join(directory, "server.key")); !os.IsNotExist(err) {
		t.Fatal("restore check generated a server private key", err)
	}
	if _, err := authority.DevelopmentServer(); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.ExistingDevelopmentServer(); err != nil {
		t.Fatal(err)
	}
}

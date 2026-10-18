package identity

import (
	"bytes"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/pki"
	"testing"
	"time"
)

func TestRenewalValidationFailureLeavesOriginalCertificateKeyAndACK(t *testing.T) {
	directory := t.TempDir()
	authority, err := pki.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	encodedCSR, err := CSR(directory)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(encodedCSR)
	if err != nil {
		t.Fatal(err)
	}
	encoded, leaf, err := authority.IssueNode(csr, "node-one")
	if err != nil {
		t.Fatal(err)
	}
	leaf.NotAfter = time.Now().Add(7 * 24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, authority.Certificate, csr.PublicKey, authority.Key)
	if err != nil {
		t.Fatal(err)
	}
	original := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := SaveEnrollment(directory, "node-one", original, authority.PEM); err != nil {
		t.Fatal(err)
	}
	state, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(23); err != nil {
		t.Fatal(err)
	}
	keyBefore, _ := os.ReadFile(filepath.Join(directory, "client.key"))
	stateBefore, _ := os.ReadFile(filepath.Join(directory, "identity.json"))
	foreign, err := pki.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherCSR, err := CSR(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := pki.ParseCSR(otherCSR)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey, _, err := authority.IssueNode(other, "node-one")
	if err != nil {
		t.Fatal(err)
	}
	wrongNode, _, err := authority.IssueNode(csr, "node-two")
	if err != nil {
		t.Fatal(err)
	}
	foreignCert, _, err := foreign.IssueNode(csr, "node-one")
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []struct {
		name            string
		certificate, ca []byte
	}{{"wrong-key", wrongKey, authority.PEM}, {"wrong-node", wrongNode, authority.PEM}, {"foreign-ca", foreignCert, foreign.PEM}, {"no-extension", original, authority.PEM}, {"invalid-pem", []byte("invalid"), authority.PEM}} {
		t.Run(fault.name, func(t *testing.T) {
			if err := state.SaveRenewal(fault.certificate, fault.ca); err == nil {
				t.Fatal("invalid rotation accepted")
			}
			certificate, _ := os.ReadFile(filepath.Join(directory, "client.crt"))
			key, _ := os.ReadFile(filepath.Join(directory, "client.key"))
			saved, _ := os.ReadFile(filepath.Join(directory, "identity.json"))
			if !bytes.Equal(certificate, original) || !bytes.Equal(key, keyBefore) || !bytes.Equal(saved, stateBefore) {
				t.Fatal("failed renewal modified current material")
			}
		})
	}
	if due, err := state.RenewalDue(time.Now()); err != nil || !due {
		t.Fatal("near expiry renewal was not scheduled", due, err)
	}
	if err := state.SaveRenewal(encoded, authority.PEM); err != nil {
		t.Fatal(err)
	}
	if due, err := state.RenewalDue(time.Now()); err != nil || due {
		t.Fatal("fresh certificate is scheduled repeatedly", due, err)
	}
}

func TestRenewalCSRDoesNotRecreateMissingOriginalPrivateKey(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "absent")
	if _, err := RenewalCSR(directory); !os.IsNotExist(err) {
		t.Fatal("missing original key did not stop CSR", err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("renewal created identity directory", err)
	}
	if _, err := CSR(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "client.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := RenewalCSR(directory); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "client.key")); !os.IsNotExist(err) {
		t.Fatal("renewal generated a replacement private key", err)
	}
}

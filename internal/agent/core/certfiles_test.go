package core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

// nodeCertificate is one settings path as the panel writes it: always under the
// canonical node directory, whatever directory the Agent reads.
func nodeCertificate(name string) string { return desiredstate.CertificateDirectory + "/" + name }

// certificateFixture creates the node's certificate directory under a private
// root, so the escape cases have somewhere to point at.
func certificateFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "certs")
	if err := os.Mkdir(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	return root, directory
}

// writeCertificatePair writes one self-signed pair into directory and returns its
// two file names. The validity is a parameter so the expired case needs no clock
// control.
func writeCertificatePair(t *testing.T, directory, name string, notBefore, notAfter time.Time) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificateFile, keyFile := name+".crt", name+".key"
	if err := os.WriteFile(filepath.Join(directory, certificateFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, keyFile), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certificateFile, keyFile
}

// The Agent is the first place that sees the files, so it proves what the core
// will need: both files exist, both are regular files inside the certificate
// directory, and they load as one pair that has not expired.
func TestCheckCertificateFilesProvesThePairOnTheNode(t *testing.T) {
	root, directory := certificateFixture(t)
	now := time.Now()
	certificate, key := writeCertificatePair(t, directory, "entry", now.Add(-time.Hour), now.Add(24*time.Hour))
	if err := checkCertificateFiles(nodeCertificate(certificate), nodeCertificate(key), directory); err != nil {
		t.Fatalf("a valid pair was refused: %v", err)
	}
	if err := checkCertificateFiles("", "", directory); err != nil {
		t.Fatalf("settings without files need no proof: %v", err)
	}
	if err := checkCertificateFiles(nodeCertificate(certificate), "", directory); err == nil {
		t.Fatal("half a pair was accepted")
	}
	if err := checkCertificateFiles(nodeCertificate("absent.crt"), nodeCertificate(key), directory); err == nil || !strings.Contains(err.Error(), "missing or unreadable") {
		t.Fatalf("a missing certificate was accepted: %v", err)
	}
	if err := os.Mkdir(filepath.Join(directory, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := checkCertificateFiles(nodeCertificate("folder"), nodeCertificate(key), directory); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a directory was accepted as a certificate: %v", err)
	}
	expiredCertificate, expiredKey := writeCertificatePair(t, directory, "expired", now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	if err := checkCertificateFiles(nodeCertificate(expiredCertificate), nodeCertificate(expiredKey), directory); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("an expired certificate was accepted: %v", err)
	}
	otherCertificate, _ := writeCertificatePair(t, directory, "other", now.Add(-time.Hour), now.Add(24*time.Hour))
	if err := checkCertificateFiles(nodeCertificate(otherCertificate), nodeCertificate(key), directory); err == nil {
		t.Fatal("a mismatched key was accepted")
	}
	// A path that leaves the directory is refused even when the file it names
	// exists out there: the panel may only point at one directory.
	escapeCertificate, _ := writeCertificatePair(t, root, "escape", now.Add(-time.Hour), now.Add(24*time.Hour))
	if err := checkCertificateFiles(nodeCertificate("../"+escapeCertificate), nodeCertificate(key), directory); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("a file outside the directory was accepted: %v", err)
	}
}

// A symlink is the other way a legal-looking path could point at another file, so
// the resolved target has to stay inside the certificate directory.
func TestCheckCertificateFilesRefusesASymlinkOutsideTheDirectory(t *testing.T) {
	root, directory := certificateFixture(t)
	outside := filepath.Join(root, "elsewhere")
	if err := os.Mkdir(outside, 0o750); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	certificate, key := writeCertificatePair(t, directory, "entry", now.Add(-time.Hour), now.Add(24*time.Hour))
	outsideCertificate, _ := writeCertificatePair(t, outside, "outside", now.Add(-time.Hour), now.Add(24*time.Hour))
	if err := os.Symlink(filepath.Join(outside, outsideCertificate), filepath.Join(directory, "link.crt")); err != nil {
		t.Skipf("this platform cannot create symlinks: %v", err)
	}
	if err := checkCertificateFiles(nodeCertificate("link.crt"), nodeCertificate(key), directory); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("a symlink leaving the directory was accepted: %v", err)
	}
	if err := os.Symlink(filepath.Join(directory, certificate), filepath.Join(directory, "inside.crt")); err != nil {
		t.Skipf("this platform cannot create symlinks: %v", err)
	}
	if err := checkCertificateFiles(nodeCertificate("inside.crt"), nodeCertificate(key), directory); err != nil {
		t.Fatalf("a symlink inside the directory was refused: %v", err)
	}
}

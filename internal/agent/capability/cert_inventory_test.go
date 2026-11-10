package capability

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func generateTestCert(t *testing.T, commonName string, dnsNames []string, ips []net.IP, notBefore, notAfter time.Time, key any) ([]byte, []byte) {
	t.Helper()
	template := x509.Certificate{
		SerialNumber: big.NewInt(100),
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:   notBefore,
		NotAfter:    notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    dnsNames,
		IPAddresses: ips,
	}

	var pubKey any
	var privPEM []byte
	switch k := key.(type) {
	case *rsa.PrivateKey:
		pubKey = &k.PublicKey
		privPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	case *ecdsa.PrivateKey:
		pubKey = &k.PublicKey
		b, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		privPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, pubKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return certPEM, privPEM
}

func TestInventoryListsPairsAndSubjects(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// 1. RSA cert + .key
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	cert1PEM, key1PEM := generateTestCert(t, "example.com", []string{"example.com", "www.example.com"}, []net.IP{net.ParseIP("192.0.2.1")}, now.Add(-time.Hour), now.Add(24*time.Hour), rsaKey)
	if err := os.WriteFile(filepath.Join(dir, "example.crt"), cert1PEM, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "example.key"), key1PEM, 0600); err != nil {
		t.Fatal(err)
	}

	// 2. certbot style fullchain.pem + privkey.pem
	rsaKey2, _ := rsa.GenerateKey(rand.Reader, 2048)
	cert2PEM, key2PEM := generateTestCert(t, "bot.example.org", []string{"bot.example.org"}, nil, now.Add(-time.Hour), now.Add(48*time.Hour), rsaKey2)
	if err := os.WriteFile(filepath.Join(dir, "fullchain.pem"), cert2PEM, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "privkey.pem"), key2PEM, 0600); err != nil {
		t.Fatal(err)
	}

	summaries := CertificateInventory(dir, now)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	// Verify example.crt
	var s1 *pb.CertificateFileSummary
	for _, s := range summaries {
		if strings.HasSuffix(s.CertificatePath, "example.crt") {
			s1 = s
			break
		}
	}
	if s1 == nil {
		t.Fatal("missing example.crt summary")
	}
	if !strings.HasSuffix(s1.KeyPath, "example.key") {
		t.Fatalf("expected keyPath to end with example.key, got %s", s1.KeyPath)
	}
	if s1.Subject != "CN=example.com" {
		t.Fatalf("expected Subject CN=example.com, got %s", s1.Subject)
	}
	if len(s1.DnsNames) != 2 || s1.DnsNames[0] != "example.com" || s1.DnsNames[1] != "www.example.com" {
		t.Fatalf("unexpected DNS names: %v", s1.DnsNames)
	}
	if len(s1.IpAddresses) != 1 || s1.IpAddresses[0] != "192.0.2.1" {
		t.Fatalf("unexpected IP addresses: %v", s1.IpAddresses)
	}
	block, _ := pem.Decode(cert1PEM)
	sum := sha256.Sum256(block.Bytes)
	expectedFp := hex.EncodeToString(sum[:])
	if s1.FingerprintSha256 != expectedFp {
		t.Fatalf("expected fingerprint %s, got %s", expectedFp, s1.FingerprintSha256)
	}

	// Verify fullchain.pem
	var s2 *pb.CertificateFileSummary
	for _, s := range summaries {
		if strings.HasSuffix(s.CertificatePath, "fullchain.pem") {
			s2 = s
			break
		}
	}
	if s2 == nil {
		t.Fatal("missing fullchain.pem summary")
	}
	if !strings.HasSuffix(s2.KeyPath, "privkey.pem") {
		t.Fatalf("expected keyPath to end with privkey.pem, got %s", s2.KeyPath)
	}
}

func TestInventoryPairsECAndPKCS8(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// EC key
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	certEC, keyEC := generateTestCert(t, "ec.example.com", []string{"ec.example.com"}, nil, now.Add(-time.Hour), now.Add(24*time.Hour), ecKey)
	if err := os.WriteFile(filepath.Join(dir, "ec.pem"), certEC, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ec.key"), keyEC, 0600); err != nil {
		t.Fatal(err)
	}

	// PKCS#8 key
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	certPKCS8, _ := generateTestCert(t, "pkcs8.example.com", []string{"pkcs8.example.com"}, nil, now.Add(-time.Hour), now.Add(24*time.Hour), rsaKey)
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPKCS8PEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes})
	if err := os.WriteFile(filepath.Join(dir, "pkcs8.pem"), certPKCS8, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkcs8.key"), keyPKCS8PEM, 0600); err != nil {
		t.Fatal(err)
	}

	// Cert without matching key
	rsaKey3, _ := rsa.GenerateKey(rand.Reader, 2048)
	certNoKey, _ := generateTestCert(t, "nokey.example.com", []string{"nokey.example.com"}, nil, now.Add(-time.Hour), now.Add(24*time.Hour), rsaKey3)
	if err := os.WriteFile(filepath.Join(dir, "nokey.pem"), certNoKey, 0644); err != nil {
		t.Fatal(err)
	}

	summaries := CertificateInventory(dir, now)
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}

	for _, s := range summaries {
		if strings.HasSuffix(s.CertificatePath, "ec.pem") {
			if !strings.HasSuffix(s.KeyPath, "ec.key") {
				t.Fatalf("expected EC key paired, got %s", s.KeyPath)
			}
		} else if strings.HasSuffix(s.CertificatePath, "pkcs8.pem") {
			if !strings.HasSuffix(s.KeyPath, "pkcs8.key") {
				t.Fatalf("expected PKCS#8 key paired, got %s", s.KeyPath)
			}
		} else if strings.HasSuffix(s.CertificatePath, "nokey.pem") {
			if s.KeyPath != "" {
				t.Fatalf("expected empty keyPath for nokey, got %s", s.KeyPath)
			}
		}
	}
}

func TestInventoryIgnoresLinksOutside(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	now := time.Now()

	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	certPEM, keyPEM := generateTestCert(t, "outside.example.com", []string{"outside.example.com"}, nil, now.Add(-time.Hour), now.Add(24*time.Hour), rsaKey)

	// Put real cert in outside directory, link inside
	outsideCert := filepath.Join(outside, "target.crt")
	if err := os.WriteFile(outsideCert, certPEM, 0644); err != nil {
		t.Fatal(err)
	}
	linkCert := filepath.Join(dir, "link.crt")
	if err := os.Symlink(outsideCert, linkCert); err != nil {
		t.Skip("symlinks not supported in environment")
	}

	// Inside cert linking to outside key
	insideCert := filepath.Join(dir, "inside.crt")
	if err := os.WriteFile(insideCert, certPEM, 0644); err != nil {
		t.Fatal(err)
	}
	outsideKey := filepath.Join(outside, "target.key")
	if err := os.WriteFile(outsideKey, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	linkKey := filepath.Join(dir, "inside.key")
	if err := os.Symlink(outsideKey, linkKey); err != nil {
		t.Skip("symlinks not supported in environment")
	}

	summaries := CertificateInventory(dir, now)
	for _, s := range summaries {
		if strings.HasSuffix(s.CertificatePath, "link.crt") {
			t.Fatal("inventory should not include cert linking outside")
		}
		if strings.HasSuffix(s.CertificatePath, "inside.crt") {
			if s.KeyPath != "" {
				t.Fatalf("inventory should not pair with key linking outside, got %s", s.KeyPath)
			}
		}
	}
}

func TestInventoryMissingDirectory(t *testing.T) {
	summaries := CertificateInventory(filepath.Join(t.TempDir(), "nonexistent"), time.Now())
	if len(summaries) != 0 {
		t.Fatalf("expected empty slice for missing directory, got %d", len(summaries))
	}
}

func TestInventoryCapsAt64(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	for i := 0; i < 70; i++ {
		name := "cert" + strings.Repeat("a", i)
		certPEM, _ := generateTestCert(t, name+".com", []string{name + ".com"}, nil, now.Add(-time.Hour), now.Add(24*time.Hour), rsaKey)
		if err := os.WriteFile(filepath.Join(dir, name+".crt"), certPEM, 0644); err != nil {
			t.Fatal(err)
		}
	}

	summaries := CertificateInventory(dir, now)
	if len(summaries) != 64 {
		t.Fatalf("expected exactly 64 summaries, got %d", len(summaries))
	}
}

func TestInventoryDigestStable(t *testing.T) {
	s1 := &pb.CertificateFileSummary{
		CertificatePath:   "/etc/proxy-agent/certs/a.crt",
		KeyPath:           "/etc/proxy-agent/certs/a.key",
		FingerprintSha256: "hash-a",
	}
	s2 := &pb.CertificateFileSummary{
		CertificatePath:   "/etc/proxy-agent/certs/b.crt",
		KeyPath:           "/etc/proxy-agent/certs/b.key",
		FingerprintSha256: "hash-b",
	}

	d1 := inventoryDigest([]*pb.CertificateFileSummary{s1, s2})
	d2 := inventoryDigest([]*pb.CertificateFileSummary{s2, s1})

	if d1 != d2 {
		t.Fatalf("expected identical digest regardless of order, got %s != %s", d1, d2)
	}
}

func TestInventoryNeverCarriesKeyBytes(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	certPEM, keyPEM := generateTestCert(t, "example.com", []string{"example.com"}, nil, now.Add(-time.Hour), now.Add(24*time.Hour), rsaKey)
	if err := os.WriteFile(filepath.Join(dir, "example.crt"), certPEM, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "example.key"), keyPEM, 0600); err != nil {
		t.Fatal(err)
	}

	summaries := CertificateInventory(dir, now)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	data, err := proto.Marshal(summaries[0])
	if err != nil {
		t.Fatal(err)
	}

	rawKeyBlock, _ := pem.Decode(keyPEM)
	keyB64 := base64.StdEncoding.EncodeToString(rawKeyBlock.Bytes[:32])
	if strings.Contains(string(data), keyB64) {
		t.Fatal("proto marshal output contains private key bytes")
	}
	for i := 0; i+16 <= len(rawKeyBlock.Bytes); i += 16 {
		if strings.Contains(string(data), string(rawKeyBlock.Bytes[i:i+16])) {
			t.Fatal("proto marshal output contains raw private key fragments")
		}
	}
}

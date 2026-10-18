// Package testutil supplies ephemeral certificates for transport tests.
package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/config"
	"testing"
	"time"
)

type PKI struct {
	Server   config.TLS
	Client   config.TLS
	RootOnly *tls.Config
}

func Certificates(t *testing.T) PKI {
	t.Helper()
	directory := t.TempDir()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(directory, "ca.crt")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(caPath, caPEM, 0600); err != nil {
		t.Fatal(err)
	}
	issue := func(name string, number int64, usage x509.ExtKeyUsage) config.TLS {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		certificate := &x509.Certificate{SerialNumber: big.NewInt(number), Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		if usage == x509.ExtKeyUsageServerAuth {
			certificate.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
			certificate.DNSNames = []string{"localhost"}
		}
		der, err := x509.CreateCertificate(rand.Reader, certificate, root, &key.PublicKey, rootKey)
		if err != nil {
			t.Fatal(err)
		}
		private, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		certPath, keyPath := filepath.Join(directory, name+".crt"), filepath.Join(directory, name+".key")
		if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
			t.Fatal(err)
		}
		return config.TLS{Certificate: certPath, Key: keyPath, ClientCA: caPath}
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	return PKI{Server: issue("server", 2, x509.ExtKeyUsageServerAuth), Client: issue("client", 3, x509.ExtKeyUsageClientAuth), RootOnly: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}
}

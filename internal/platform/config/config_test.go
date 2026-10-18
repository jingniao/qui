package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSelfSigned writes a server certificate and key in directory and returns
// their base names so a relative-path config can point at them.
func writeSelfSigned(t *testing.T, directory string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "panel"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "server.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "server.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	return "server.crt", "server.key"
}

func TestPanelConfigLoadsWithoutClientCA(t *testing.T) {
	directory := t.TempDir()
	certificate, key := writeSelfSigned(t, directory)
	path := filepath.Join(directory, "panel.yaml")
	write := func(clientCA string) {
		t.Helper()
		content := "development: false\nhttp_address: 0.0.0.0:443\ngrpc_address: 0.0.0.0:9443\ndatabase: data/panel.db\nnode_identity_directory: identity\ntls:\n  certificate: " + certificate + "\n  key: " + key + "\n"
		if clientCA != "" {
			content += "  client_ca: " + clientCA + "\n"
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("")
	c, err := LoadPanel(path)
	if err != nil {
		t.Fatalf("config without client_ca did not load: %v", err)
	}
	if c.TLS.ClientCA != "" {
		t.Fatal("unexpected client_ca resolved")
	}
	write(certificate)
	if _, err := LoadPanel(path); err != nil {
		t.Fatalf("config with client_ca no longer loads: %v", err)
	}
}

func TestDevelopmentCannotBindPublicAddress(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8080", "localhost:8080", "example.com:8080", ":8080"} {
		c := DefaultPanel()
		c.HTTPAddress = address
		if c.Validate() == nil {
			t.Fatalf("accepted insecure public address %q", address)
		}
	}
	c := DefaultPanel()
	c.HTTPAddress = "[::1]:8080"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadStrictAndRelativePaths(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(path, []byte("database: data/panel.db\nshutdown_timeout: 3s\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadPanel(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Database != filepath.Join(directory, "data/panel.db") {
		t.Fatal(c.Database)
	}
	for _, data := range []string{"unknown_option: true\n", "database: data/panel.db\n---\ndevelopment: true\n", "shutdown_timeout: 0s\n", "development: false\n"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPanel(path); err == nil {
			t.Fatalf("accepted invalid config: %s", data)
		}
	}
}

func TestTrustedProxiesValidation(t *testing.T) {
	c := DefaultPanel()
	c.Server.TrustedProxies = []string{"10.0.0.0/8", " 192.0.2.1 "}
	networks, err := c.Server.ParsedTrustedProxies()
	if err != nil || len(networks) != 2 {
		t.Fatalf("valid trusted proxies rejected: %v", err)
	}
	if !networks[1].Contains(net.ParseIP("192.0.2.1")) || networks[1].Contains(net.ParseIP("192.0.2.2")) {
		t.Fatal("bare IP not treated as a single-host network")
	}
	if err := c.ValidateStructure(); err != nil {
		t.Fatal(err)
	}
	c.Server.TrustedProxies = []string{"not-a-network"}
	if err := c.ValidateStructure(); err == nil {
		t.Fatal("accepted invalid trusted proxy")
	}
}

func TestAgentRejectsMissingTLS(t *testing.T) {
	c := DefaultAgent()
	c.Development = false
	if c.Validate() == nil {
		t.Fatal("accepted production agent without mTLS identity")
	}
}

func TestAgentCoreSelectionDefaultsToXrayAndRejectsUndefinedCore(t *testing.T) {
	c := DefaultAgent()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.CoreName = "sing-box"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.CoreName = "untrusted-shell"
	if err := c.Validate(); err == nil {
		t.Fatal("undefined runtime selected")
	}
}

func TestPanelRecoveryPathLoadDefersTLSWithoutAcceptingInvalidFinalConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "panel.yaml")
	if err := os.WriteFile(path, []byte("development: false\nhttp_address: 0.0.0.0:443\ngrpc_address: 0.0.0.0:9443\ndatabase: data/panel.db\nnode_identity_directory: identity\ntls:\n  certificate: tls/server.crt\n  key: tls/server.key\n  client_ca: tls/ca.crt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadPanelPaths(path)
	if err != nil {
		t.Fatal("recovery cannot locate data before incomplete TLS is repaired", err)
	}
	if err := c.ValidateStructure(); err != nil {
		t.Fatal(err)
	}
	if c.Database != filepath.Join(root, "data", "panel.db") || c.TLS.Key != filepath.Join(root, "tls", "server.key") {
		t.Fatal("recovery paths not resolved")
	}
	if _, err := LoadPanel(path); err == nil {
		t.Fatal("production startup accepted missing TLS after deferred load")
	}
}

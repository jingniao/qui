package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/pkg/contracts/release"
)

func TestFetchToolUsesEnrolledIdentityConfiguredPinsAndRetainsOriginalStores(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := filepath.Join(directory, "node")
	identityDirectory := filepath.Join(dataDirectory, "identity")
	authority, err := pki.LoadOrCreate(filepath.Join(directory, "authority"))
	if err != nil {
		t.Fatal(err)
	}
	csrPEM, err := identity.CSR(identityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(csrPEM)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	certificate, _, err := authority.IssueNode(csr, "release-fetch-node")
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.SaveEnrollment(identityDirectory, "release-fetch-node", certificate, authority.PEM); err != nil {
		t.Fatal(err)
	}
	configurationKey := bytes.Repeat([]byte{0x56}, 32)
	if err := os.WriteFile(filepath.Join(dataDirectory, "configuration.key"), configurationKey, 0600); err != nil {
		t.Fatal(err)
	}
	// These are unrelated stores. Fetch must leave them byte-for-byte intact.
	if err := os.Mkdir(filepath.Join(dataDirectory, "spool"), 0700); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(dataDirectory, "spool", "accounting-test.enc")
	if err := os.WriteFile(spool, []byte("original-accounting-store"), 0600); err != nil {
		t.Fatal(err)
	}
	originalIdentity, err := os.ReadFile(filepath.Join(identityDirectory, "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	hash := sha256.Sum256(artifact)
	now := time.Now().UTC()
	signed, err := release.Sign(release.Manifest{SchemaVersion: 1, Component: "proxy-agent", Version: "fetch-test", OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: 4, Bytes: int64(len(artifact)), SHA256: hex.EncodeToString(hash[:]), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}, private)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(signed)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) != 0 {
			t.Error("node private identity used for release download")
		}
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			w.Write(manifest)
		} else {
			w.Write(artifact)
		}
	}))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(directory, "catalogue-ca.crt"), ca, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "agent.yaml")
	yaml := fmt.Sprintf("data_directory: node\nupdates:\n  catalogue: %s/releases\n  ca: catalogue-ca.crt\n  keys:\n    - public_key: %s\n      components: [proxy-agent]\n", server.URL, base64.StdEncoding.EncodeToString(public))
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	common := []string{"fetch", "--agent-config", path, "--component", "proxy-agent", "--sequence", "4"}
	output := &bytes.Buffer{}
	if err := run(common, output); err == nil {
		t.Fatal("missing history auto-initialized")
	}
	if err := run(append(append([]string{}, common...), "--initialize-history"), output); err != nil {
		t.Fatal(err)
	}
	if err := run(common, output); err != nil {
		t.Fatal("durable retry failed", err)
	}
	if err := run(append(append([]string{}, common...), "--initialize-history"), output); err == nil {
		t.Fatal("existing history reset")
	}
	old := []string{"fetch", "--agent-config", path, "--component", "proxy-agent", "--sequence", "3"}
	if err := run(old, output); err == nil {
		t.Fatal("accepted lower release sequence")
	}
	for _, item := range []struct {
		path string
		want []byte
	}{
		{filepath.Join(identityDirectory, "client.key"), originalIdentity},
		{filepath.Join(dataDirectory, "configuration.key"), configurationKey},
		{spool, []byte("original-accounting-store")},
	} {
		got, err := os.ReadFile(item.path)
		if err != nil || !bytes.Equal(got, item.want) {
			t.Fatal("fetch modified identity or accounting", item.path, err)
		}
	}
	if bytes.Contains(output.Bytes(), bytes.TrimSpace(originalIdentity)) {
		t.Fatal("node private key printed")
	}
}

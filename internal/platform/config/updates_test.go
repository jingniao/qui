package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentReleaseTrustIsStrictLocalOptionalConfiguration(t *testing.T) {
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	encoded := base64.StdEncoding.EncodeToString(public)
	directory := t.TempDir()
	path := filepath.Join(directory, "agent.yaml")
	data := "updates:\n  catalogue: https://releases.example.com/proxy-control\n  keys:\n    - public_key: " + encoded + "\n      components: [proxy-agent, xray]\n      revoked: false\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadAgent(path)
	if err != nil || !c.Updates.Enabled() || len(c.Updates.Keys) != 1 {
		t.Fatal(c.Updates, err)
	}
	trust, err := c.Updates.TrustConfig()
	if err != nil || len(trust.Certificates) != 0 || trust.RootCAs != nil {
		t.Fatal("download inherited node credentials", err)
	}
	for _, bad := range []string{
		strings.Replace(data, "https://", "http://", 1),
		strings.Replace(data, "[proxy-agent, xray]", "[shell]", 1),
		strings.Replace(data, "revoked: false", "revoked: false\n      unknown: true", 1),
		"updates:\n  catalogue: https://example.com\n",
		"updates:\n  ca: missing-ca.crt\n",
	} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadAgent(path); err == nil {
			t.Fatal("bad release configuration accepted", bad)
		}
	}
	if err := os.WriteFile(path, []byte("updates: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = LoadAgent(path)
	if err != nil || c.Updates.Enabled() {
		t.Fatal("ordinary Agent now requires update trust", err)
	}
}

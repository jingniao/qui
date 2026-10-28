package user

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"testing"
	"time"
)

func testRepository(t *testing.T) *Repository {
	t.Helper()
	directory := t.TempDir()
	database, err := db.Open(context.Background(), filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	configurationVault, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	return &Repository{Database: database, Vault: configurationVault}
}
func userInput() Input {
	return Input{Username: "test_user", DisplayName: "Test", Status: "active", AllowedProtocols: []string{"socks5", "vless", "wireguard"}}
}
func TestCredentialFormatsAndUserEligibility(t *testing.T) {
	secret, err := GenerateCredential("wireguard")
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(secret.Value)
	if err != nil {
		t.Fatal(err)
	}
	private, err := ecdh.X25519().NewPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()) != secret.PublicKey {
		t.Fatal("WireGuard keypair mismatch")
	}
	uuid, err := GenerateCredential("vless")
	if err != nil || len(uuid.Value) != 36 || uuid.Value[14] != '4' {
		t.Fatal("invalid VLESS UUID")
	}
	u := User{Status: "active", TrafficLimit: 100, UsedTraffic: 99}
	if !u.Active(time.Now()) {
		t.Fatal("active user rejected")
	}
	u.UsedTraffic = 100
	if u.Active(time.Now()) {
		t.Fatal("quota exceeded user accepted")
	}
	u.TrafficLimit = 0
	u.ExpiresAt = timestamp(time.Now().Add(-time.Second))
	if u.Active(time.Now()) {
		t.Fatal("expired user accepted")
	}
	encoded, _ := json.Marshal(Credential{ID: "test", Status: "active"})
	if bytes.Contains(encoded, []byte("secret")) {
		t.Fatal("masked secret field included")
	}
}

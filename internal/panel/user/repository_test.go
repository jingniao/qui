package user

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"sync"
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
	return Input{Username: "test_user", DisplayName: "Test", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"socks5", "vless", "wireguard"}}
}
func TestDeviceCredentialsAreIndependentEncryptedAndMasked(t *testing.T) {
	ctx := context.Background()
	repository := testRepository(t)
	u, err := repository.Save(ctx, "", userInput())
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Laptop", Protocols: []string{"socks5", "vless", "wireguard"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Phone", Protocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Credentials[0].Secret == second.Credentials[0].Secret || first.Credentials[0].Username == second.Credentials[0].Username {
		t.Fatal("devices reused credentials")
	}
	masked, err := repository.Device(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range masked.Credentials {
		if credential.Secret != "" {
			t.Fatal("device response exposed credential")
		}
	}
	var sealed []byte
	if err := repository.Database.QueryRow("SELECT encrypted_secret FROM credentials WHERE id=?", first.Credentials[0].ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(first.Credentials[0].Secret)) {
		t.Fatal("credential stored unencrypted")
	}
	replaced, err := repository.RotateCredential(ctx, first.ID, "socks5", false)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Secret == first.Credentials[0].Secret || replaced.ID == first.Credentials[0].ID {
		t.Fatal("rotation reused credentials")
	}
	if _, err := repository.RotateCredential(ctx, first.ID, "vless", true); err != nil {
		t.Fatal(err)
	}
	active, err := repository.Credentials(ctx, first.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range active {
		if credential.Protocol == "vless" && credential.Status != "revoked" {
			t.Fatal("protocol revocation failed")
		}
		if credential.Status == "revoked" && credential.Secret != "" {
			t.Fatal("revoked credential revealed")
		}
	}
	if _, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Third", Protocols: []string{"socks5"}}); !errors.Is(err, ErrDeviceLimit) {
		t.Fatalf("device limit bypassed: %v", err)
	}
	if _, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Unsupported", Protocols: []string{"trojan"}}); err == nil {
		t.Fatal("user protocol whitelist bypassed")
	}
	if err := repository.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := repository.Database.QueryRow("SELECT count(*) FROM credentials").Scan(&count); err != nil || count != 0 {
		t.Fatal("user deletion left credentials", err)
	}
}
func TestConcurrentCreationCannotExceedDeviceLimit(t *testing.T) {
	ctx := context.Background()
	repository := testRepository(t)
	input := userInput()
	input.MaxDevices = 1
	u, err := repository.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Concurrent", Protocols: []string{"socks5"}})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrDeviceLimit) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected one created device, got %d", successes)
	}
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

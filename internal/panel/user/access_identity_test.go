package user

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"
)

// Since v1.1 each user has one access identity. Credentials of two users are
// independent, stored encrypted, masked in normal reads and rotatable.
func TestIdentityCredentialsAreIndependentEncryptedAndMasked(t *testing.T) {
	ctx := context.Background()
	repository := testRepository(t)
	u, err := repository.Save(ctx, "", userInput())
	if err != nil {
		t.Fatal(err)
	}
	otherInput := userInput()
	otherInput.Username = "other_user"
	other, err := repository.Save(ctx, "", otherInput)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Laptop", Protocols: []string{"socks5", "vless", "wireguard"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.CreateDevice(ctx, other.ID, DeviceInput{Name: "Phone", Protocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Credentials[0].Secret == second.Credentials[0].Secret || first.Credentials[0].Username == second.Credentials[0].Username {
		t.Fatal("users reused credentials")
	}
	again, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Tablet", Protocols: []string{"socks5"}})
	if err != nil || again.ID != first.ID {
		t.Fatalf("a second device was created instead of returning the identity: %v %v", again.ID, err)
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
	if _, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Unsupported", Protocols: []string{"trojan"}}); err == nil {
		t.Fatal("user protocol whitelist bypassed")
	}
	if err := repository.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := repository.Database.QueryRow("SELECT count(*) FROM credentials WHERE user_id=?", u.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("user deletion left credentials", err)
	}
}

// Concurrent callers all get the same identity; the user never has two.
func TestConcurrentCallsKeepOneIdentity(t *testing.T) {
	ctx := context.Background()
	repository := testRepository(t)
	u, err := repository.Save(ctx, "", userInput())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan string, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			device, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Concurrent", Protocols: []string{"socks5"}})
			if err != nil {
				t.Error(err)
				return
			}
			results <- device.ID
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for id := range results {
		seen[id] = true
	}
	var devices int
	if err := repository.Database.QueryRow("SELECT count(*) FROM devices WHERE user_id=?", u.ID).Scan(&devices); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || devices != 1 {
		t.Fatalf("identities %v, devices %d", seen, devices)
	}
}

// A5.6 (repository part): a user is created with one credential per allowed
// protocol; a protocol change issues the new one and revokes the removed one; a
// reset re-issues every credential; a second device left over from an older
// database is removed with its credentials tombstoned.
func TestAccessIdentityFollowsTheUser(t *testing.T) {
	ctx := context.Background()
	repository := testRepository(t)
	u, err := repository.Save(ctx, "", userInput())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := repository.AccessIdentity(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	active := func() map[string]string {
		credentials, err := repository.Credentials(ctx, identity.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		result := map[string]string{}
		for _, credential := range credentials {
			if credential.Status == "active" {
				result[credential.Protocol] = credential.ID
			}
		}
		return result
	}
	initial := active()
	if len(initial) != 3 || initial["socks5"] == "" || initial["vless"] == "" || initial["wireguard"] == "" {
		t.Fatalf("credentials after create: %v", initial)
	}
	input := userInput()
	input.AllowedProtocols = []string{"socks5", "trojan"}
	if _, err := repository.Save(ctx, u.ID, input); err != nil {
		t.Fatal(err)
	}
	changed := active()
	if len(changed) != 2 || changed["socks5"] != initial["socks5"] || changed["trojan"] == "" {
		t.Fatalf("credentials after the protocol change: %v", changed)
	}
	if again, err := repository.EnsureAccessIdentity(ctx, u.ID); err != nil || again {
		t.Fatalf("a consistent identity reported a change: %t %v", again, err)
	}
	if err := repository.ResetCredentials(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	reset := active()
	if len(reset) != 2 || reset["socks5"] == changed["socks5"] || reset["trojan"] == changed["trojan"] {
		t.Fatalf("credentials after the reset: %v (before %v)", reset, changed)
	}
	stamp := timestamp(time.Now())
	if _, err := repository.Database.Exec(`INSERT INTO devices(id,user_id,name,status,created_at) VALUES('extra-device',?,'old phone','active',?)`, u.ID, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Database.Exec(`INSERT INTO credentials(id,user_id,device_id,protocol,username,encrypted_secret,status,created_at) VALUES('extra-credential',?,'extra-device','socks5','x',x'00','active',?)`, u.ID, stamp); err != nil {
		t.Fatal(err)
	}
	if changed, err := repository.EnsureAccessIdentity(ctx, u.ID); err != nil || !changed {
		t.Fatalf("the extra device was not removed: %t %v", changed, err)
	}
	var devices, tombstones int
	repository.Database.QueryRow("SELECT count(*) FROM devices WHERE user_id=?", u.ID).Scan(&devices)
	repository.Database.QueryRow(`SELECT count(*) FROM credential_tombstones WHERE credential_id='extra-credential' AND device_id='extra-device'`).Scan(&tombstones)
	if devices != 1 || tombstones != 1 {
		t.Fatalf("devices %d, tombstones %d", devices, tombstones)
	}
}

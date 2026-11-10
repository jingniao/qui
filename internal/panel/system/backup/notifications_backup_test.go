package backup

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebhookFormatsArchiveInspection(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	for i, format := range []string{"", "generic", "wecom", "dingtalk", "feishu", "invalid"} {
		config := fmt.Sprintf(`{"url":"https://example.com/hook","format":%q}`, format)
		if format == "dingtalk" || format == "feishu" {
			config = fmt.Sprintf(`{"url":"https://example.com/hook","format":%q,"secret":"signing-key"}`, format)
		}
		encrypted, err := source.ConfigurationVault.Seal([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := source.Database.Exec("INSERT OR REPLACE INTO notification_channels(id,kind,name,enabled,encrypted_config,created_at,updated_at) VALUES('archive_formats','webhook','Formats',1,?,'now','now')", encrypted); err != nil {
			t.Fatal(err)
		}
		created, err := source.Create(ctx, directory, fmt.Sprintf("webhook_formats_%02d", i), "owner", testPassword)
		if format == "invalid" {
			if err == nil {
				t.Fatal("invalid format accepted during archive creation/inspection")
			}
			continue
		}
		if err != nil {
			t.Fatalf("create %q: %v", format, err)
		}
		if _, err := Inspect(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword); err != nil {
			t.Fatalf("inspect %q: %v", format, err)
		}
	}
}

func TestBackupWebhookFormatValidation(t *testing.T) {
	source, _, _ := sourceFixture(t)
	for _, tc := range []struct {
		kind, config string
		valid        bool
	}{
		{"webhook", `{"url":"https://example.com"}`, true},
		{"webhook", `{"url":"https://example.com","format":"generic"}`, true},
		{"webhook", `{"url":"https://example.com","format":"wecom"}`, true},
		{"webhook", `{"url":"https://example.com","format":"dingtalk","secret":"SEC-key"}`, true},
		{"webhook", `{"url":"https://example.com","format":"feishu","secret":"key"}`, true},
		{"webhook", `{"url":"https://example.com","format":"invalid"}`, false},
		{"webhook", `{"url":"https://example.com","format":"wecom","secret":"bad"}`, false},
		{"webhook", `{"url":"https://example.com","format":"feishu","secret":"` + strings.Repeat("x", 1025) + `"}`, false},
		{"telegram", `{"botToken":"bot","chatId":"chat","format":"generic"}`, false},
		{"webhook", `{"url":"https://example.com","unknown":true}`, false},
	} {
		encrypted, err := source.ConfigurationVault.Seal([]byte(tc.config))
		if err != nil {
			t.Fatal(err)
		}
		_, err = source.Database.Exec("INSERT OR REPLACE INTO notification_channels(id,kind,name,enabled,encrypted_config,created_at,updated_at) VALUES('formats',?,'Formats',1,?,'now','now')", tc.kind, encrypted)
		if err != nil {
			t.Fatal(err)
		}
		err = validateNotifications(context.Background(), source.Database, source.ConfigurationVault)
		if (err == nil) != tc.valid {
			t.Fatalf("config %s: valid=%v err=%v", tc.config, tc.valid, err)
		}
	}
}

func TestBackupRejectsMalformedNotifications(t *testing.T) {
	source, _, _ := sourceFixture(t)
	ctx := context.Background()

	// Initial db is valid
	if err := validateNotifications(ctx, source.Database, source.ConfigurationVault); err != nil {
		t.Fatalf("expected valid database to pass: %v", err)
	}

	// 1. Invalid channel encrypted_config (garbage ciphertext that cannot be opened by vault)
	if _, err := source.Database.Exec("INSERT INTO notification_channels(id,kind,name,enabled,encrypted_config,created_at,updated_at) VALUES('bad_ch','telegram','Bad',1,X'0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20','2026-11-09T00:00:00Z','2026-11-09T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if err := validateNotifications(ctx, source.Database, source.ConfigurationVault); err == nil {
		t.Fatal("expected unopenable channel config to fail validation")
	}
	if _, err := source.Database.Exec("DELETE FROM notification_channels WHERE id='bad_ch'"); err != nil {
		t.Fatal(err)
	}

	// 2. Invalid notification severity
	if _, err := source.Database.Exec("INSERT INTO notifications(id,at,kind,severity,title,body) VALUES(999,'2026-11-09T00:00:00Z','cpu','invalid_sev','Title','Body')"); err != nil {
		// SQLite CHECK might reject it directly
	} else {
		if err := validateNotifications(ctx, source.Database, source.ConfigurationVault); err == nil {
			t.Fatal("expected invalid notification severity to fail validation")
		}
		source.Database.Exec("DELETE FROM notifications WHERE id=999")
	}

	// 3. Invalid rule duration / threshold (e.g. cpu threshold > 100)
	if _, err := source.Database.Exec("UPDATE notification_rules SET threshold=150 WHERE kind='cpu'"); err != nil {
		t.Fatal(err)
	}
	if err := validateNotifications(ctx, source.Database, source.ConfigurationVault); err == nil {
		t.Fatal("expected rule threshold > 100 to fail validation")
	}
}

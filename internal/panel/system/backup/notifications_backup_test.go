package backup

import (
	"context"
	"testing"
)

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

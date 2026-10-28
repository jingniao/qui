package backup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/portal"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/user"
)

func TestPortalBackupVerifierAndInventory(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	users := &user.Repository{Database: source.Database, Vault: source.ConfigurationVault}
	accounts, _, err := users.List(ctx, "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	u := accounts[0]
	hash, err := auth.HashPassword("backup-portal-password-1")
	if err != nil {
		t.Fatal(err)
	}
	r := &portal.Repository{Database: source.Database}
	if err := r.SetLogin(ctx, u.ID, true, hash); err != nil {
		t.Fatal(err)
	}
	if err := r.TouchLogin(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateSession(ctx, auth.HashToken("portal-backup-session"), u.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	identity, err := users.AccessIdentity(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	subscriptions := &subscription.Repository{Database: source.Database, Vault: source.ConfigurationVault}
	_, token, err := subscriptions.Rotate(ctx, u.ID, identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	created, err := source.Create(ctx, directory, "portal_backup_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, work string) error {
		inventory, err := InventoryFromVerifiedDirectory(ctx, work)
		if err != nil {
			return err
		}
		if inventory.Totals.PortalSessions != 1 {
			t.Fatalf("portal session inventory: %+v", inventory.Totals)
		}
		database, err := readonlyDatabase(filepath.Join(work, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		account, err := (&portal.Repository{Database: database}).Account(ctx, u.ID)
		if err != nil {
			return err
		}
		if !account.LoginEnabled || account.PasswordHash != hash || account.LastLoginAt == "" {
			t.Fatal("login material lost in verified backup")
		}
		link, err := (&subscription.Repository{Database: database, Vault: source.ConfigurationVault}).Link(ctx, u.ID)
		if err != nil || link != token {
			t.Fatalf("encrypted token lost: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPortalBackupVerifierRejectsMissingOrCorrupt0114Material(t *testing.T) {
	for _, query := range []string{
		"DROP TABLE user_sessions",
		"ALTER TABLE user_sessions DROP COLUMN created_at",
		"ALTER TABLE users DROP COLUMN password_hash",
		"ALTER TABLE users DROP COLUMN last_login_at",
		"ALTER TABLE subscription_tokens DROP COLUMN encrypted_token",
		"UPDATE subscription_tokens SET encrypted_token=x'0102'",
		"UPDATE subscription_tokens SET token_hash='different-hash'",
	} {
		t.Run(query, func(t *testing.T) {
			source, _, _ := sourceFixture(t)
			ctx := context.Background()
			users := &user.Repository{Database: source.Database, Vault: source.ConfigurationVault}
			accounts, _, err := users.List(ctx, "", 1, 10)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := users.AccessIdentity(ctx, accounts[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := (&subscription.Repository{Database: source.Database, Vault: source.ConfigurationVault}).Rotate(ctx, accounts[0].ID, identity.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := source.Database.Exec(query); err != nil {
				t.Fatal(err)
			}
			if err := validatePortal(ctx, source.Database, source.ConfigurationVault); !errors.Is(err, ErrArchive) {
				t.Fatalf("damaged material accepted: %v", err)
			}
		})
	}
}

func TestPortalBackupVerifierAcceptsPre0114Material(t *testing.T) {
	source, _, _ := sourceFixture(t)
	ctx := context.Background()
	// Model an older archive without changing any migration source or checksum.
	for _, query := range []string{"DROP TABLE user_sessions", "ALTER TABLE users DROP COLUMN password_hash", "ALTER TABLE users DROP COLUMN login_enabled", "ALTER TABLE users DROP COLUMN last_login_at", "ALTER TABLE subscription_tokens DROP COLUMN encrypted_token", "DELETE FROM schema_migrations WHERE version='0114_end_user_accounts.sql'"} {
		if _, err := source.Database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := validatePortal(ctx, source.Database, source.ConfigurationVault); err != nil {
		t.Fatal(err)
	}
	i, err := ReadInventory(ctx, source.Database)
	if err != nil || i.Totals.PortalSessions != 0 {
		t.Fatalf("pre-0114 inventory: %+v %v", i.Totals, err)
	}
}

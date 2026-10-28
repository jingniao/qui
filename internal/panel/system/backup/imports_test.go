package backup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/user"
)

func TestImportBackupVerifierAndInventory(t *testing.T) {
	s, directory, _ := sourceFixture(t)
	ctx := context.Background()
	users := &user.Repository{Database: s.Database, Vault: s.ConfigurationVault}
	accounts, _, err := users.List(ctx, "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	store := &importer.Store{Database: s.Database, Vault: s.ConfigurationVault}
	key, err := store.Save(ctx, "", importer.Input{Name: "external fixture", Content: "socks5://fixture:fixture@example.com:1080#external", Interval: 60, UserIDs: []string{accounts[0].ID}})
	if err != nil || key == "" {
		t.Fatal(err)
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(ctx, directory, "imports_backup_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, work string) error {
		inventory, e := InventoryFromVerifiedDirectory(ctx, work)
		if e != nil {
			return e
		}
		if inventory.Totals.SubscriptionSources != 1 || inventory.Totals.ImportedProxies != 1 || inventory.Totals.SourceGrants != 1 {
			t.Fatal("import inventory missing")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec("UPDATE imported_proxies SET payload_encrypted=x'0102'"); err != nil {
		t.Fatal(err)
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
		t.Fatal("corrupt imported payload accepted")
	}
}
func TestImportBackupVerifierAcceptsPre0116(t *testing.T) {
	s, _, _ := sourceFixture(t)
	for _, q := range []string{"DROP TABLE user_source_grants", "DROP TABLE imported_proxies", "DROP TABLE subscription_sources", "ALTER TABLE canvas_schemes DROP COLUMN client_published_layout", "DELETE FROM schema_migrations WHERE version='0116_subscription_sources.sql'"} {
		if _, err := s.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateImports(context.Background(), s.Database, s.ConfigurationVault); err != nil {
		t.Fatal(err)
	}
}

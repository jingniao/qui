package backup

import (
	"context"
	"encoding/json"
	"errors"
	"net"
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
	key, _, err := store.Save(ctx, "", importer.Input{Name: "external fixture", Content: "socks5://fixture:fixture@example.com:1080#external", Interval: 60, UserIDs: []string{accounts[0].ID}})
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
		if inventory.Totals.SubscriptionSources != 1 || inventory.Totals.ImportedProxies != 1 || inventory.Totals.SourceGrants != 0 {
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

// The stored result is validated: a payload this panel could not have written is
// refused, and an archive taken before 0133 (no result columns) stays valid.
func TestImportBackupVerifierValidatesResults(t *testing.T) {
	s, _, _ := sourceFixture(t)
	ctx := context.Background()
	store := &importer.Store{Database: s.Database, Vault: s.ConfigurationVault}
	key, _, err := store.Save(ctx, "", importer.Input{Name: "result fixture", Content: "socks5://fixture:fixture@example.com:1080#one", Interval: 60})
	if err != nil || key == "" {
		t.Fatal(err)
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec("UPDATE subscription_sources SET latest_result_json='{\"outcome\":\"ok\",\"message\":\"token=private-fixture\"}'"); err != nil {
		t.Fatal(err)
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
		t.Fatalf("invalid result accepted: %v", err)
	}
	// A pre-0133 archive has neither column and must still verify.
	for _, q := range []string{
		"ALTER TABLE subscription_sources DROP COLUMN latest_result_json",
		"ALTER TABLE subscription_sources DROP COLUMN last_success_at",
		"DELETE FROM schema_migrations WHERE version='0133_subscription_source_results.sql'",
	} {
		if _, err := s.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); err != nil {
		t.Fatalf("pre-0133 archive rejected: %v", err)
	}
}

// Every result type the panel can store verifies, and a result that does not belong
// to its own row - another source, another attempt or success time, a status it
// contradicts, a diagnostic field or an unknown key - is refused.
func TestImportBackupVerifierChecksResultAgainstRow(t *testing.T) {
	s, _, _ := sourceFixture(t)
	ctx := context.Background()
	store := &importer.Store{Database: s.Database, Vault: s.ConfigurationVault}
	ok, okResult, err := store.Save(ctx, "", importer.Input{Name: "ok fixture", Content: "socks5://fixture:fixture@example.com:1080#one", Interval: 60})
	if err != nil || okResult.Outcome != "ok" {
		t.Fatalf("ok fixture: %+v %v", okResult, err)
	}
	mixed, mixedResult, err := store.Save(ctx, "", importer.Input{Name: "mixed fixture", Content: "socks5://fixture:fixture@example.com:1080#kept\nvmess://unsupported.example.com:443", Interval: 60})
	if err != nil || mixedResult.Outcome != "partial" || len(mixedResult.Diagnostics) != 1 {
		t.Fatalf("mixed fixture: %+v %v", mixedResult, err)
	}
	// A failed attempt as the importer records it: the fetch is refused, so the row
	// carries the recorded failure with the reason the panel writes.
	linkStore := &importer.Store{Database: s.Database, Vault: s.ConfigurationVault,
		Fetcher: importer.Fetcher{DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("fixture refused")
		}}}
	failedID, _, err := linkStore.Save(ctx, "", importer.Input{Name: "failed fixture", URL: "https://1.1.1.1/fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	failedResult, err := linkStore.Refresh(ctx, failedID)
	if !errors.Is(err, importer.ErrFetch) || failedResult.Outcome != "failed" || failedResult.Code != "source_connect_failed" {
		t.Fatalf("failed fixture: %+v %v", failedResult, err)
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); err != nil {
		t.Fatalf("panel results rejected: %v", err)
	}
	original := map[string]string{}
	for _, key := range []string{ok, mixed} {
		var stored string
		if err = s.Database.QueryRow("SELECT latest_result_json FROM subscription_sources WHERE id=?", key).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		original[key] = stored
	}
	// Each tampering is refused, and the untouched row verifies again afterwards.
	tamper := func(key string, mutate func(*importer.Result)) {
		t.Helper()
		var result importer.Result
		if err = json.Unmarshal([]byte(original[key]), &result); err != nil {
			t.Fatal(err)
		}
		mutate(&result)
		tampered, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err = s.Database.Exec("UPDATE subscription_sources SET latest_result_json=? WHERE id=?", string(tampered), key); err != nil {
			t.Fatal(err)
		}
		if err = validateImports(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
			t.Fatalf("tampered result accepted: %s", tampered)
		}
		if _, err = s.Database.Exec("UPDATE subscription_sources SET latest_result_json=? WHERE id=?", original[key], key); err != nil {
			t.Fatal(err)
		}
		if err = validateImports(ctx, s.Database, s.ConfigurationVault); err != nil {
			t.Fatalf("restored result rejected: %v", err)
		}
	}
	tamper(ok, func(r *importer.Result) { r.SourceID = "another-source" })
	tamper(ok, func(r *importer.Result) { r.AttemptedAt = "x" })
	tamper(ok, func(r *importer.Result) { r.LastSuccessAt = nil })
	tamper(ok, func(r *importer.Result) { r.Outcome = "partial" })
	tamper(mixed, func(r *importer.Result) { r.Diagnostics[0].Field = "https://example.com/secret" })
	tamper(mixed, func(r *importer.Result) { r.Diagnostics[0].Message = "token=private-fixture" })
	tamper(ok, func(r *importer.Result) { cached := 5; r.Counts.Cached = &cached })
	// An ok row that keeps a failed result is refused ...
	if _, err = s.Database.Exec("UPDATE subscription_sources SET last_status='ok' WHERE id=?", failedID); err != nil {
		t.Fatal(err)
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
		t.Fatal("an ok row kept a failed result")
	}
	if _, err = s.Database.Exec("UPDATE subscription_sources SET last_status='failed' WHERE id=?", failedID); err != nil {
		t.Fatal(err)
	}
	// ... and so is a failed row that keeps a successful result.
	if _, err = s.Database.Exec("UPDATE subscription_sources SET last_status='failed' WHERE id=?", ok); err != nil {
		t.Fatal(err)
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
		t.Fatal("a failed row kept a successful result")
	}
	if _, err = s.Database.Exec("UPDATE subscription_sources SET last_status='ok' WHERE id=?", ok); err != nil {
		t.Fatal(err)
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); err != nil {
		t.Fatalf("rows rejected after restore: %v", err)
	}
}

// A Clash document whose entry fails for a Clash-specific reason is the panel's own
// data: its stored reason names a field the verifier accepts, so a real backup of the
// database is created and verified instead of being refused.
func TestImportBackupAcceptsClashFieldReasons(t *testing.T) {
	s, directory, _ := sourceFixture(t)
	ctx := context.Background()
	store := &importer.Store{Database: s.Database, Vault: s.ConfigurationVault}
	content := "proxies:\n  - {name: kept, type: socks5, server: a.example.com, port: 1080}\n" +
		"  - {name: bad, type: socks5, server: b.example.com, port: 1080, allow-insecure: true}\n"
	if _, result, err := store.Save(ctx, "", importer.Input{Name: "clash fixture", Content: content, Interval: 60}); err != nil ||
		result.Outcome != "partial" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Field != "entry" {
		t.Fatalf("clash fixture: %+v %v", result, err)
	}
	if err := validateImports(ctx, s.Database, s.ConfigurationVault); err != nil {
		t.Fatalf("the panel's own reason was refused: %v", err)
	}
	created, err := s.Create(ctx, directory, "imports_clash_fields", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(directory, created.Manifest.ID+".pcb")
	if _, err = Examine(ctx, directory, archive, testPassword, func(_ Result, work string) error {
		inventory, e := InventoryFromVerifiedDirectory(ctx, work)
		if e != nil {
			return e
		}
		if inventory.Totals.SubscriptionSources != 1 || inventory.Totals.ImportedProxies != 1 {
			t.Fatalf("the archived result was lost: %+v", inventory.Totals)
		}
		return nil
	}); err != nil {
		t.Fatalf("a backup of the panel's own row was refused: %v", err)
	}
}

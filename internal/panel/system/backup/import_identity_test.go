package backup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"proxycontrol/internal/panel/subscription/importer"
)

func importIdentityFixture(t *testing.T) (Source, string, string, string) {
	t.Helper()
	s, directory, _ := sourceFixture(t)
	store := &importer.Store{Database: s.Database, Vault: s.ConfigurationVault}
	sourceID, _, err := store.Save(context.Background(), "", importer.Input{Name: "identity fixture", Content: "socks5://fixture:fixture@example.com:1080#stable", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	var proxyID string
	if err := s.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", sourceID).Scan(&proxyID); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"UPDATE import_identity_reconcile SET done=1",
		"UPDATE imported_proxies SET revision=7",
		"INSERT INTO import_identity_map(source_id,old_proxy_id,old_proxy_key,new_proxy_id) SELECT source_id,'old-id',name||'|'||protocol,id FROM imported_proxies",
		"INSERT INTO system_settings(key,value_json) VALUES('import_identity_report','[]')",
	} {
		if _, err := s.Database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	return s, directory, sourceID, proxyID
}

func TestValidateImportIdentityRejectsCorruption(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"missing map target", "UPDATE import_identity_map SET new_proxy_id='missing'"},
		{"missing map source", "UPDATE import_identity_map SET source_id='missing'"},
		{"invalid revision", "UPDATE imported_proxies SET revision=0"},
		{"fractional revision", "UPDATE imported_proxies SET revision=1.5"},
		{"invalid report", "UPDATE system_settings SET value_json='{' WHERE key='import_identity_report'"},
		{"missing marker", "DELETE FROM import_identity_reconcile"},
		{"invalid marker", "PRAGMA ignore_check_constraints=ON; UPDATE import_identity_reconcile SET done=2"},
		{"duplicate marker", "PRAGMA ignore_check_constraints=ON; INSERT INTO import_identity_reconcile(id,done) VALUES(2,1)"},
		{"name mismatch", "UPDATE imported_proxies SET name='changed'"},
		{"protocol mismatch", "UPDATE imported_proxies SET protocol='http'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _ := importIdentityFixture(t)
			ctx := context.Background()
			if err := validateImportIdentity(ctx, s.Database, s.ConfigurationVault); err != nil {
				t.Fatalf("valid identity rejected: %v", err)
			}
			if _, err := s.Database.Exec(tc.query); err != nil {
				t.Fatal(err)
			}
			if err := validateImportIdentity(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
				t.Fatalf("corruption accepted: %v", err)
			}
		})
	}
}

func TestValidateImportIdentityMemberKeys(t *testing.T) {
	s, _, sourceID, proxyID := importIdentityFixture(t)
	ctx := context.Background()
	if _, err := s.Database.Exec("INSERT INTO node_groups(id,name,purpose,created_at) VALUES('identity-group','identity group','subscription','2026-11-13T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Database.Exec("INSERT INTO node_group_imports(group_id,source_id,proxy_key) VALUES('identity-group',?,?)", sourceID, "id:"+proxyID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key   string
		done  int
		valid bool
	}{
		// Retained unresolved legacy members and stable IDs of deleted nodes are
		// reported, legal states (decision 4), not archive corruption.
		{"id:" + proxyID, 1, true}, {"", 1, false}, {"stable|socks5", 1, true},
		{"id:missing", 1, true}, {"id:", 1, false}, {"garbage", 1, false},
		{"", 0, true}, {"stable|socks5", 0, true}, {"id:" + proxyID, 0, true},
		{"id:missing", 0, true}, {"garbage", 0, false},
	} {
		if _, err := s.Database.Exec("UPDATE import_identity_reconcile SET done=?", tc.done); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Database.Exec("UPDATE node_group_imports SET proxy_key=?", tc.key); err != nil {
			t.Fatal(err)
		}
		err := validateImportIdentity(ctx, s.Database, s.ConfigurationVault)
		if (err == nil) != tc.valid {
			t.Fatalf("key=%q done=%d valid=%v error=%v", tc.key, tc.done, tc.valid, err)
		}
	}
}

func TestValidateImportIdentityAcceptsPre0147(t *testing.T) {
	s, directory, _, _ := importIdentityFixture(t)
	rewindImportedProxyIdentity(t, s.Database)
	ctx := context.Background()
	if err := validateImportIdentity(ctx, s.Database, s.ConfigurationVault); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(ctx, directory, "pre_identity_archive", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword); err != nil {
		t.Fatal(err)
	}
}

func TestImportIdentityBackupRoundTrip(t *testing.T) {
	s, directory, sourceID, proxyID := importIdentityFixture(t)
	ctx := context.Background()
	created, err := s.Create(ctx, directory, "identity_roundtrip", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, work string) error {
		db, err := readonlyDatabase(filepath.Join(work, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		var gotID, gotSource, oldID, oldKey, mappedID string
		var revision int
		if err := db.QueryRow("SELECT id,source_id,revision FROM imported_proxies").Scan(&gotID, &gotSource, &revision); err != nil {
			return err
		}
		if gotID != proxyID || gotSource != sourceID || revision != 7 {
			t.Fatalf("identity changed: %s %s %d", gotID, gotSource, revision)
		}
		if err := db.QueryRow("SELECT old_proxy_id,old_proxy_key,new_proxy_id FROM import_identity_map").Scan(&oldID, &oldKey, &mappedID); err != nil {
			return err
		}
		if oldID != "old-id" || oldKey != "stable|socks5" || mappedID != proxyID {
			t.Fatalf("mapping changed: %s %s %s", oldID, oldKey, mappedID)
		}
		inventory, err := ReadInventory(ctx, db)
		if err != nil {
			return err
		}
		if inventory.Totals.ImportIdentityMap != 1 {
			t.Fatalf("mapping inventory=%d", inventory.Totals.ImportIdentityMap)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportIdentityGroupMemberLimit(t *testing.T) {
	s, _, sourceID, _ := importIdentityFixture(t)
	if _, err := s.Database.Exec("INSERT INTO node_groups(id,name,purpose,created_at) VALUES('identity-group','identity group','subscription','2026-11-13T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Database.Exec(`WITH RECURSIVE members(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM members WHERE n<1000)
		INSERT INTO node_group_imports(group_id,source_id,proxy_key) SELECT 'identity-group',?,'legacy-'||n||'|socks5' FROM members`, sourceID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := validateNodeGroupEntries(ctx, s.Database); err != nil {
		t.Fatalf("0147 member capacity rejected: %v", err)
	}
	if _, err := s.Database.Exec("INSERT INTO node_group_imports(group_id,source_id,proxy_key) VALUES('identity-group',?,'overflow|socks5')", sourceID); err != nil {
		t.Fatal(err)
	}
	if err := validateNodeGroupEntries(ctx, s.Database); !errors.Is(err, ErrArchive) {
		t.Fatalf("overflow accepted: %v", err)
	}
}

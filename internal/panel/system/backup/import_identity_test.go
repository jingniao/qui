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
	if _, err := s.Database.Exec("UPDATE imported_proxies SET revision=7"); err != nil {
		t.Fatal(err)
	}
	return s, directory, sourceID, proxyID
}

func TestValidateImportIdentityRejectsCorruption(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"invalid revision", "UPDATE imported_proxies SET revision=0"},
		{"fractional revision", "UPDATE imported_proxies SET revision=1.5"},
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
		valid bool
	}{
		// Stable IDs of deleted nodes and unresolved name|protocol members are
		// legal dangling states, not archive corruption.
		{"id:" + proxyID, true}, {"", false}, {"stable|socks5", true},
		{"id:missing", true}, {"id:", false}, {"garbage", false},
	} {
		if _, err := s.Database.Exec("UPDATE node_group_imports SET proxy_key=?", tc.key); err != nil {
			t.Fatal(err)
		}
		err := validateImportIdentity(ctx, s.Database, s.ConfigurationVault)
		if (err == nil) != tc.valid {
			t.Fatalf("key=%q valid=%v error=%v", tc.key, tc.valid, err)
		}
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
		var gotID, gotSource string
		var revision int
		if err := db.QueryRow("SELECT id,source_id,revision FROM imported_proxies").Scan(&gotID, &gotSource, &revision); err != nil {
			return err
		}
		if gotID != proxyID || gotSource != sourceID || revision != 7 {
			t.Fatalf("identity changed: %s %s %d", gotID, gotSource, revision)
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

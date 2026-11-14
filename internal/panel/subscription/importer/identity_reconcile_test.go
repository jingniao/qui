package importer

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestReconcileImportedIdentitiesRollbackRetryAndOnce(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	src, _, err := s.Save(ctx, "", Input{Name: "batch", Content: identityContent, Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	var old string
	if err = s.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&old); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("reference write failed")
	calls := 0
	callback := func(ctx context.Context, tx *sql.Tx, resolve func(string, string) ([]string, bool), refs map[string]string) ([]string, error) {
		calls++
		if refs[old] == "" || refs[old] == old {
			t.Fatal("missing identity mapping")
		}
		ids, content := resolve(src, "Original|socks5")
		if !content || len(ids) != 1 || ids[0] != refs[old] {
			t.Fatalf("resolved %v %v", ids, content)
		}
		return []string{"fixture dangling reference"}, failure
	}
	if err = s.ReconcileImportedIdentities(ctx, callback); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	var current string
	var done, count int
	if err := s.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if err := s.Database.QueryRow("SELECT done FROM import_identity_reconcile").Scan(&done); err != nil {
		t.Fatal(err)
	}
	if err := s.Database.QueryRow("SELECT count(*) FROM import_identity_map").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if current != old || done != 0 || count != 0 {
		t.Fatalf("partial commit: %s %d %d", current, done, count)
	}
	failure = nil
	if err = s.ReconcileImportedIdentities(ctx, callback); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileImportedIdentities(ctx, callback); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("callback ran %d times", calls)
	}
	var report string
	if err = s.Database.QueryRow("SELECT value_json FROM system_settings WHERE key='import_identity_report'").Scan(&report); err != nil {
		t.Fatal(err)
	}
	if report != `["fixture dangling reference"]` {
		t.Fatalf("report=%s", report)
	}
}

func TestReconcileImportedIdentitiesRejectsCorruptData(t *testing.T) {
	for _, field := range []string{"url_encrypted", "payload_encrypted", "name", "protocol"} {
		t.Run(field, func(t *testing.T) {
			s, _, _ := storeFixture(t)
			ctx := context.Background()
			src, _, err := s.Save(ctx, "", Input{Name: "batch", Content: identityContent, Interval: 60})
			if err != nil {
				t.Fatal(err)
			}
			table, where := "imported_proxies", "source_id"
			if field == "url_encrypted" {
				table, where = "subscription_sources", "id"
			}
			if _, err = s.Database.Exec("UPDATE "+table+" SET "+field+"=? WHERE "+where+"=?", "corrupt", src); err != nil {
				t.Fatal(err)
			}
			if err = s.ReconcileImportedIdentities(ctx); err == nil {
				t.Fatal("accepted corrupt data")
			}
			if field == "url_encrypted" || field == "payload_encrypted" {
				_, original := s.Vault.Open([]byte("corrupt"))
				if err.Error() != original.Error() {
					t.Fatalf("vault error changed: %v, original %v", err, original)
				}
			}
			var done int
			if err = s.Database.QueryRow("SELECT done FROM import_identity_reconcile").Scan(&done); err != nil || done != 0 {
				t.Fatalf("done=%d err=%v", done, err)
			}
		})
	}
}

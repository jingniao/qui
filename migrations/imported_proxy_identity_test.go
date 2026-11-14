package migrations

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestImportedProxyIdentityMigration(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, query := range []string{
		"PRAGMA foreign_keys=ON",
		"CREATE TABLE users(id TEXT PRIMARY KEY)",
		"CREATE TABLE canvas_schemes(id TEXT PRIMARY KEY)",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	applyWhole(t, database, "0116_subscription_sources.sql")
	if _, err := database.Exec("INSERT INTO subscription_sources(id,name,url_encrypted,interval_seconds,created_at,updated_at) VALUES('source','source',x'0102',60,'created','updated')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at) VALUES('old-id','source','Old name','trojan',x'0304','fetched')"); err != nil {
		t.Fatal(err)
	}
	applyWhole(t, database, "0147_imported_proxy_identity.sql")

	t.Run("existing row preserved", func(t *testing.T) {
		var id, sourceID, name, protocol, fetchedAt string
		var payload []byte
		var revision int
		err := database.QueryRow("SELECT id,source_id,name,protocol,payload_encrypted,fetched_at,revision FROM imported_proxies").Scan(&id, &sourceID, &name, &protocol, &payload, &fetchedAt, &revision)
		if err != nil {
			t.Fatal(err)
		}
		if id != "old-id" || sourceID != "source" || name != "Old name" || protocol != "trojan" || !reflect.DeepEqual(payload, []byte{3, 4}) || fetchedAt != "fetched" || revision != 1 {
			t.Fatalf("row changed: %q %q %q %q %x %q revision=%d", id, sourceID, name, protocol, payload, fetchedAt, revision)
		}
	})
	t.Run("new row revision", func(t *testing.T) {
		if _, err := database.Exec("INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at) VALUES('new-id','source','New name','trojan',x'0506','later')"); err != nil {
			t.Fatal(err)
		}
		var revision int
		if err := database.QueryRow("SELECT revision FROM imported_proxies WHERE id='new-id'").Scan(&revision); err != nil || revision != 1 {
			t.Fatalf("revision=%d err=%v", revision, err)
		}
		if _, err := database.Exec("UPDATE imported_proxies SET revision=2 WHERE id='new-id'"); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec("UPDATE imported_proxies SET revision=NULL WHERE id='new-id'"); err == nil {
			t.Fatal("accepted null revision")
		}
	})
	t.Run("reconcile marker", func(t *testing.T) {
		var count, id, done int
		if err := database.QueryRow("SELECT count(*),id,done FROM import_identity_reconcile").Scan(&count, &id, &done); err != nil || count != 1 || id != 1 || done != 0 {
			t.Fatalf("count=%d id=%d done=%d err=%v", count, id, done, err)
		}
		for _, query := range []string{
			"INSERT INTO import_identity_reconcile(id,done) VALUES(1,0)",
			"INSERT INTO import_identity_reconcile(id,done) VALUES(2,0)",
			"UPDATE import_identity_reconcile SET done=2",
			"UPDATE import_identity_reconcile SET done=NULL",
		} {
			if _, err := database.Exec(query); err == nil {
				t.Fatalf("accepted invalid reconcile marker: %s", query)
			}
		}
		if _, err := database.Exec("UPDATE import_identity_reconcile SET done=1 WHERE id=1"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("identity map", func(t *testing.T) {
		rows, err := database.Query("SELECT * FROM import_identity_map")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		rows.Close()
		if err != nil || !reflect.DeepEqual(columns, []string{"source_id", "old_proxy_id", "old_proxy_key", "new_proxy_id"}) {
			t.Fatalf("map columns=%v err=%v", columns, err)
		}
		insert := "INSERT INTO import_identity_map(source_id,old_proxy_id,old_proxy_key,new_proxy_id) VALUES(?,?,?,?)"
		for _, args := range [][]any{
			{"source", "old-a", "Same name|trojan", "new-a"},
			{"source", "old-b", "Same name|trojan", "new-b"},
		} {
			if _, err := database.Exec(insert, args...); err != nil {
				t.Fatal(err)
			}
		}
		for _, args := range [][]any{
			{"source", "old-a", "Other name|trojan", "new-c"},
			{"source", "old-c", "Other name|trojan", "new-a"},
			{"source", "old-c", nil, "new-c"},
		} {
			if _, err := database.Exec(insert, args...); err == nil {
				t.Fatalf("accepted ambiguous or incomplete mapping: %v", args)
			}
		}
	})
}

package migrations

import (
	"database/sql"
	"testing"
)

func TestRelayCleanupMigrationPreservesReuseInbound(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		"PRAGMA foreign_keys=ON", "CREATE TABLE nodes(id TEXT PRIMARY KEY)",
		"CREATE TABLE inbounds(id TEXT PRIMARY KEY,default_outbound TEXT)",
		"CREATE TABLE canvas_entry_bindings(inbound_id TEXT,mode TEXT)",
		"INSERT INTO nodes VALUES('n')", "INSERT INTO inbounds VALUES('i','keep-route')",
		"INSERT INTO canvas_entry_bindings VALUES('i','reuse'),('j','create')",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	applyWhole(t, db, "0150_relay_cleanup.sql")
	var route string
	if err := db.QueryRow("SELECT default_outbound FROM inbounds WHERE id='i'").Scan(&route); err != nil || route != "keep-route" {
		t.Fatal(route, err)
	}
	var count, completed int
	if err := db.QueryRow("SELECT count(*) FROM canvas_entry_bindings WHERE mode='reuse'").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM canvas_entry_bindings WHERE mode='create'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := db.QueryRow("SELECT completed FROM relay_cleanup_reconcile WHERE id=1").Scan(&completed); err != nil || completed != 0 {
		t.Fatal(completed, err)
	}
	if _, err := db.Exec("INSERT INTO relay_cleanup_nodes(node_id,reason,next_attempt_at,created_at,updated_at) VALUES('n','remove','2026-11-15T00:00:00Z','2026-11-15T00:00:00Z','2026-11-15T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"UPDATE relay_cleanup_nodes SET attempts=-1", "UPDATE relay_cleanup_nodes SET revision_id=0", "UPDATE relay_cleanup_reconcile SET completed=2", "INSERT INTO relay_cleanup_reconcile VALUES(2,0)"} {
		if _, err := db.Exec(q); err == nil {
			t.Fatal("accepted invalid cleanup state", q)
		}
	}
	if _, err := db.Exec("DELETE FROM nodes WHERE id='n'"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM relay_cleanup_nodes").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

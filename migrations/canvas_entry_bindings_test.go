package migrations

import (
	"database/sql"
	"testing"
)

func TestCanvasEntryBindingsMigration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"PRAGMA foreign_keys=ON", "CREATE TABLE canvas_schemes(id TEXT PRIMARY KEY)", "CREATE TABLE inbounds(id TEXT PRIMARY KEY)", "INSERT INTO canvas_schemes VALUES('s')", "INSERT INTO inbounds VALUES('i'),('j')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	applyWhole(t, db, "0146_canvas_entry_bindings.sql")
	insert := "INSERT INTO canvas_entry_bindings(id,scheme_id,instance_id,inbound_id,mode,original_default_outbound,state,request_id,updated_at) VALUES(?,?,?,?,?,?,?,?, 'now')"
	if _, err := db.Exec(insert, "b", "s", "entry", "i", "reuse", "direct", "active", "request"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]any{
		{"c", "s", "other", "i", "create", "", "active", nil},
		{"c", "s", "other", "j", "bad", "", "active", nil},
		{"c", "s", "other", "j", "reuse", "", "active", nil},
		{"c", "s", "entry", "j", "create", "", "active", nil},
		{"c", "s", "other", "j", "create", "", "active", "request"},
	} {
		if _, err := db.Exec(insert, args...); err == nil {
			t.Fatalf("accepted invalid binding: %v", args)
		}
	}
	if _, err := db.Exec("DELETE FROM inbounds WHERE id='i'"); err == nil {
		t.Fatal("deleted managed inbound")
	}
	if _, err := db.Exec("DELETE FROM canvas_schemes WHERE id='s'"); err != nil {
		t.Fatal(err)
	}
	var scheme sql.NullString
	var state string
	if err := db.QueryRow("SELECT scheme_id,state FROM canvas_entry_bindings WHERE id='b'").Scan(&scheme, &state); err != nil || scheme.Valid || state != "detached" {
		t.Fatalf("scheme=%v state=%q err=%v", scheme, state, err)
	}
	if _, err := db.Exec(insert, "c", nil, "entry", "j", "create", "", "detached", nil); err != nil {
		t.Fatal(err)
	}
}

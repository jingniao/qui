package backup

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func TestValidateCanvasEntryBindings(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"CREATE TABLE schema_migrations(version TEXT)", "CREATE TABLE inbounds(id TEXT)", "CREATE TABLE canvas_schemes(id TEXT)", "INSERT INTO inbounds VALUES('i')", "INSERT INTO canvas_schemes VALUES('s')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if err := validateCanvasEntryBindings(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"INSERT INTO schema_migrations VALUES('0146_canvas_entry_bindings.sql')", "CREATE TABLE canvas_entry_bindings(mode TEXT,original_default_outbound TEXT,inbound_id TEXT,scheme_id TEXT,instance_id TEXT DEFAULT 'entry',state TEXT,applied_default_outbound TEXT DEFAULT '')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		mode, original, inbound string
		scheme                  any
		valid                   bool
	}{
		{"create", "", "i", "s", true}, {"reuse", "direct", "i", nil, true},
		{"bad", "", "i", "s", false}, {"reuse", "", "i", "s", false},
		{"create", "", "missing", "s", false}, {"create", "", "i", "missing", false},
	} {
		if _, err := db.Exec("DELETE FROM canvas_entry_bindings"); err != nil {
			t.Fatal(err)
		}
		state := "applied"
		if tc.scheme == nil {
			state = "detached"
		}
		if _, err := db.Exec("INSERT INTO canvas_entry_bindings(mode,original_default_outbound,inbound_id,scheme_id,state) VALUES(?,?,?,?,?)", tc.mode, tc.original, tc.inbound, tc.scheme, state); err != nil {
			t.Fatal(err)
		}
		if err := validateCanvasEntryBindings(ctx, db); (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}

func TestValidateCanvasEntryBindingsArchiveSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, mutate string
		valid        bool
	}{
		{"applied_create", "", true},
		{"detached_reuse", "UPDATE canvas_entry_bindings SET scheme_id=NULL,state='detached',mode='reuse',original_default_outbound='direct'", true},
		{"unknown_state", "UPDATE canvas_entry_bindings SET state='published'", false},
		{"empty_state", "UPDATE canvas_entry_bindings SET state=''", false},
		{"null_state", "UPDATE canvas_entry_bindings SET state=NULL", false},
		{"null_scheme_applied", "UPDATE canvas_entry_bindings SET scheme_id=NULL", false},
		{"owned_detached", "UPDATE canvas_entry_bindings SET state='detached'", false},
		{"duplicate_instance", "INSERT INTO canvas_entry_bindings SELECT mode,original_default_outbound,'i2',scheme_id,instance_id,state,applied_default_outbound FROM canvas_entry_bindings", false},
		{"instance_in_different_scheme", "INSERT INTO canvas_entry_bindings SELECT mode,original_default_outbound,'i2','s2',instance_id,state,applied_default_outbound FROM canvas_entry_bindings", true},
		{"detached_instances", "UPDATE canvas_entry_bindings SET scheme_id=NULL,state='detached'; INSERT INTO canvas_entry_bindings SELECT mode,original_default_outbound,'i2',scheme_id,instance_id,state,applied_default_outbound FROM canvas_entry_bindings", true},
		{"create_original", "UPDATE canvas_entry_bindings SET original_default_outbound='direct'", false},
		{"null_original", "UPDATE canvas_entry_bindings SET original_default_outbound=NULL", false},
		{"null_applied", "UPDATE canvas_entry_bindings SET applied_default_outbound=NULL", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := canvasBindingArchiveDB(t)
			if tc.mutate != "" {
				if _, err := db.Exec(tc.mutate); err != nil {
					t.Fatal(err)
				}
			}
			assertCanvasBindingArchive(t, db, tc.valid)
		})
	}
}

func TestValidateCanvasEntryBindingsRouteEncoding(t *testing.T) {
	for _, column := range []string{"original_default_outbound", "applied_default_outbound"} {
		for _, tc := range []struct {
			value string
			valid bool
		}{
			{"direct", true}, {"block", true}, {"missing-outbound_1.id", true},
			{"chain:missing-chain", true}, {"group:missing-group", true},
			{"chain:", false}, {"group:", false}, {"outbound:id", false},
			// No contract fixes the outbound ID alphabet, so only control
			// characters are rejected; an unusual ID is not archive corruption.
			{"chain:a:b", true}, {"a/b", true}, {"bad\n", false}, {"chain:bad\x00", false},
		} {
			t.Run(column+"/"+tc.value, func(t *testing.T) {
				db := canvasBindingArchiveDB(t)
				if _, err := db.Exec("UPDATE canvas_entry_bindings SET mode='reuse',original_default_outbound='direct',"+column+"=?", tc.value); err != nil {
					t.Fatal(err)
				}
				assertCanvasBindingArchive(t, db, tc.valid)
			})
		}
	}
}

func canvasBindingArchiveDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		"CREATE TABLE schema_migrations(version TEXT)",
		"INSERT INTO schema_migrations VALUES('0146_canvas_entry_bindings.sql')",
		"CREATE TABLE inbounds(id TEXT)", "INSERT INTO inbounds VALUES('i'),('i2')",
		"CREATE TABLE canvas_schemes(id TEXT)", "INSERT INTO canvas_schemes VALUES('s'),('s2')",
		"CREATE TABLE canvas_entry_bindings(mode TEXT,original_default_outbound TEXT,inbound_id TEXT,scheme_id TEXT,instance_id TEXT,state TEXT,applied_default_outbound TEXT)",
		"INSERT INTO canvas_entry_bindings VALUES('create','','i','s','entry','applied','')",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func assertCanvasBindingArchive(t *testing.T, db *sql.DB, valid bool) {
	t.Helper()
	err := validateCanvasEntryBindings(context.Background(), db)
	if valid && err != nil {
		t.Fatal(err)
	}
	if !valid && !errors.Is(err, ErrArchive) {
		t.Fatalf("want ErrArchive, got %v", err)
	}
}

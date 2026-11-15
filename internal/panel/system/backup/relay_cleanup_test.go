package backup

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestValidateRelayCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, mutate string
		valid        bool
	}{
		{"pending", "", true},
		{"completed", "UPDATE relay_cleanup_reconcile SET completed=1", true},
		{"missing_node", "UPDATE relay_cleanup_nodes SET node_id='missing'", false},
		{"negative_attempts", "UPDATE relay_cleanup_nodes SET attempts=-1", false},
		{"bad_timestamp", "UPDATE relay_cleanup_nodes SET next_attempt_at='invalid'", false},
		{"empty_reason", "UPDATE relay_cleanup_nodes SET reason=''", false},
		{"missing_revision", "UPDATE relay_cleanup_nodes SET revision_id=99", false},
		{"missing_marker", "DELETE FROM relay_cleanup_reconcile", false},
		{"invalid_marker", "UPDATE relay_cleanup_reconcile SET completed=2", false},
		{"null_marker_id", "UPDATE relay_cleanup_reconcile SET id=NULL", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			for _, q := range []string{
				"CREATE TABLE schema_migrations(version TEXT)", "INSERT INTO schema_migrations VALUES('0150_relay_cleanup.sql')",
				"CREATE TABLE nodes(id TEXT)", "INSERT INTO nodes VALUES('n')", "CREATE TABLE config_revisions(id INTEGER)", "INSERT INTO config_revisions VALUES(1)",
				"CREATE TABLE relay_cleanup_reconcile(id INTEGER,completed INTEGER)", "INSERT INTO relay_cleanup_reconcile VALUES(1,0)",
				"CREATE TABLE relay_cleanup_nodes(node_id TEXT,reason TEXT,attempts INTEGER,last_code TEXT,next_attempt_at TEXT,created_at TEXT,updated_at TEXT,revision_id INTEGER)",
				"INSERT INTO relay_cleanup_nodes VALUES('n','removed',2,'apply_failed','2026-11-15T00:05:00Z','2026-11-15T00:00:00Z','2026-11-15T00:00:00Z',1)",
			} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if tc.mutate != "" {
				if _, err := db.Exec(tc.mutate); err != nil {
					t.Fatal(err)
				}
			}
			err = validateRelayCleanup(context.Background(), db)
			if tc.valid && err != nil || !tc.valid && !errors.Is(err, ErrArchive) {
				t.Fatal(err)
			}
		})
	}
}

func TestRelayCleanupBackupRestoreRoundTrip(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	if _, err := source.Database.Exec(`INSERT INTO relay_cleanup_nodes(node_id,reason,attempts,last_code,next_attempt_at,created_at,updated_at,revision_id)
SELECT id,'scheme removed',2,'apply_failed','2026-11-15T00:05:00Z','2026-11-15T00:00:00Z','2026-11-15T00:00:00Z',1 FROM nodes`); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Database.Exec("UPDATE relay_cleanup_reconcile SET completed=1"); err != nil {
		t.Fatal(err)
	}
	created, err := source.Create(ctx, directory, "relay_cleanup_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRestore(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, restoreTargetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := DiscardPreparedRestore(ctx, prepared); err != nil {
			t.Error(err)
		}
	}()
	db, err := readonlyDatabase(filepath.Join(prepared.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := validateRelayCleanup(ctx, db); err != nil {
		t.Fatal(err)
	}
	var reason, code, next string
	var attempts, revision, completed int
	if err := db.QueryRow("SELECT reason,attempts,last_code,next_attempt_at,revision_id FROM relay_cleanup_nodes").Scan(&reason, &attempts, &code, &next, &revision); err != nil || reason != "scheme removed" || attempts != 2 || code != "apply_failed" || next != "2026-11-15T00:05:00Z" || revision != 1 {
		t.Fatal(reason, attempts, code, next, revision, err)
	}
	if err := db.QueryRow("SELECT completed FROM relay_cleanup_reconcile").Scan(&completed); err != nil || completed != 1 {
		t.Fatal(completed, err)
	}
	inventory, err := ReadInventory(ctx, db)
	if err != nil || inventory.Totals.RelayCleanupNodes != 1 || inventory.Totals.RelayCleanupReconcile != 1 {
		t.Fatal(inventory.Totals, err)
	}
}

package migrations

import (
	"database/sql"
	"testing"
)

func TestCanvasEntryBaselineMigration(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err = database.Exec("CREATE TABLE canvas_entry_bindings(id TEXT PRIMARY KEY); INSERT INTO canvas_entry_bindings VALUES('old')"); err != nil {
		t.Fatal(err)
	}
	applyWhole(t, database, "0148_canvas_entry_baseline.sql")
	var config, groups *string
	if err = database.QueryRow("SELECT creation_config_json,creation_groups_json FROM canvas_entry_bindings WHERE id='old'").Scan(&config, &groups); err != nil {
		t.Fatal(err)
	}
	if config != nil || groups != nil {
		t.Fatal("migration guessed a historical baseline")
	}
	if _, err = database.Exec(`INSERT INTO canvas_entry_bindings VALUES('new','{}','[]')`); err != nil {
		t.Fatal(err)
	}
}

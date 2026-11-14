package backup

import (
	"database/sql"
	"testing"
)

func rewindCanvasEntryBaseline(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0148_canvas_entry_baseline.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, q := range []string{"ALTER TABLE canvas_entry_bindings DROP COLUMN creation_config_json", "ALTER TABLE canvas_entry_bindings DROP COLUMN creation_groups_json", "DELETE FROM schema_migrations WHERE version='0148_canvas_entry_baseline.sql'"} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

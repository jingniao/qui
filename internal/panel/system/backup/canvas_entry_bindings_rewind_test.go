package backup

import (
	"database/sql"
	"testing"
)

func rewindCanvasEntryBindings(t *testing.T, database *sql.DB) {
	t.Helper()
	rewindImportedProxyIdentity(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0146_canvas_entry_bindings.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{"DROP TABLE canvas_entry_bindings", "DELETE FROM schema_migrations WHERE version='0146_canvas_entry_bindings.sql'"} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

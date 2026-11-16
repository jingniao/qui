package backup

import (
	"database/sql"
	"testing"
)

// rewindNodeGroupRelays removes 0151 before constructing an older archive.
func rewindNodeGroupRelays(t *testing.T, database *sql.DB) {
	t.Helper()
	rewindTunnelImportTarget(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0151_node_group_relays.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{"DROP TABLE node_group_relays", "DELETE FROM schema_migrations WHERE version='0151_node_group_relays.sql'"} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

package recovery

import (
	"database/sql"
	"testing"
)

// rewindRelayCleanup removes 0150 before constructing an older archive. Reuse
// bindings, if needed, must be inserted afterwards: migration deleted them.
func rewindRelayCleanup(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0150_relay_cleanup.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DROP TABLE relay_cleanup_nodes",
		"DROP TABLE relay_cleanup_reconcile",
		"DELETE FROM schema_migrations WHERE version='0150_relay_cleanup.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

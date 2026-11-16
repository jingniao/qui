package backup

import (
	"database/sql"
	"testing"
)

func rewindRelayCleanup(t *testing.T, db *sql.DB) {
	t.Helper()
	rewindNodeGroupRelays(t, db)
	var applied int
	if err := db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0150_relay_cleanup.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, q := range []string{"DROP TABLE relay_cleanup_nodes", "DROP TABLE relay_cleanup_reconcile", "DELETE FROM schema_migrations WHERE version='0150_relay_cleanup.sql'"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

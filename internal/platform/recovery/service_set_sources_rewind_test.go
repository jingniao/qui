package recovery

import (
	"database/sql"
	"testing"
)

// rewindServiceSetSources removes migration 0140 so a fixture can stand in for
// an archive written before it.
func rewindServiceSetSources(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0140_service_set_sources.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DROP TABLE service_set_sources",
		"CREATE TABLE service_set_entries_old (id INTEGER PRIMARY KEY, service_id TEXT NOT NULL REFERENCES service_sets(id) ON DELETE CASCADE, kind TEXT NOT NULL CHECK(kind IN ('domain','suffix','cidr')), value TEXT NOT NULL, UNIQUE(service_id,kind,value))",
		"INSERT INTO service_set_entries_old(id,service_id,kind,value) SELECT id,service_id,kind,value FROM service_set_entries WHERE kind != 'keyword'",
		"DROP TABLE service_set_entries",
		"ALTER TABLE service_set_entries_old RENAME TO service_set_entries",
		"DELETE FROM schema_migrations WHERE version='0140_service_set_sources.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

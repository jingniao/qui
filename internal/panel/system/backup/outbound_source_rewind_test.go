package backup

import (
	"database/sql"
	"testing"
)

func rewindOutboundSource(t *testing.T, database *sql.DB) {
	t.Helper()
	rewindCanvasEntryBindings(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0145_outbound_source.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{"ALTER TABLE outbounds DROP COLUMN source_ip", "ALTER TABLE outbounds DROP COLUMN source_interface", "DELETE FROM schema_migrations WHERE version='0145_outbound_source.sql'"} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

package recovery

import (
	"database/sql"
	"testing"
)

// rewindInboundTLSSource removes migration 0136 so a fixture can stand in for an
// archive written before it. Every older-archive fixture here reaches this helper
// through rewindIdentityRecovery; migrating forward again re-applies 0136, which
// is why the column goes as well.
func rewindInboundTLSSource(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0136_inbound_tls_source.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"ALTER TABLE inbounds DROP COLUMN tls_source",
		"DELETE FROM schema_migrations WHERE version='0136_inbound_tls_source.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

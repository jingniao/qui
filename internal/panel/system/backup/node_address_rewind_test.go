package backup

import (
	"database/sql"
	"testing"
)

// rewindNodeConnectAddress removes migration 0135 so a fixture can stand in for
// an archive written before it. An archive's applied migrations must be a
// contiguous prefix of this binary's list, so every older-archive fixture here
// reaches this helper through rewindIdentityRecovery; migrating forward again
// re-applies 0135, which is why the two columns go as well.
func rewindNodeConnectAddress(t *testing.T, database *sql.DB) {
	t.Helper()
	// Every fixture that rewinds below 0135 passes through here, so the newest
	// migration is dropped first.
	rewindCertificates(t, database)
	rewindInboundTLSSource(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0135_node_connect_address.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"ALTER TABLE nodes DROP COLUMN public_ip_manual",
		"ALTER TABLE nodes DROP COLUMN connect_host",
		"DELETE FROM schema_migrations WHERE version='0135_node_connect_address.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

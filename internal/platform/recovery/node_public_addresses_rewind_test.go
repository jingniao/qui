package recovery

import (
	"database/sql"
	"testing"
)

// rewindNodePublicAddresses removes migration 0138 so a fixture can stand in for
// an archive written before it.
func rewindNodePublicAddresses(t *testing.T, database *sql.DB) {
	t.Helper()
	rewindNotifications(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0138_node_public_addresses.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"ALTER TABLE nodes DROP COLUMN public_addresses_json",
		"ALTER TABLE nodes DROP COLUMN public_ip_selected",
		"DELETE FROM schema_migrations WHERE version='0138_node_public_addresses.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

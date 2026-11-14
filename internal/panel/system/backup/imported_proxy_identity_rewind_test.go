package backup

import (
	"database/sql"
	"testing"
)

// rewindImportedProxyIdentity removes migration 0147 (imported proxy stable
// identities) so a fixture can stand in for an archive written before it.
func rewindImportedProxyIdentity(t *testing.T, database *sql.DB) {
	t.Helper()
	rewindCanvasEntryBaseline(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0147_imported_proxy_identity.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DROP TABLE import_identity_reconcile",
		"DROP TABLE import_identity_map",
		"ALTER TABLE imported_proxies DROP COLUMN revision",
		"DELETE FROM schema_migrations WHERE version='0147_imported_proxy_identity.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

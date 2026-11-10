package backup

import (
	"database/sql"
	"testing"
)

// rewindCertificates removes migration 0137 so a fixture can stand in for an
// archive written before it. The column's index has to go before the column
// itself, and the table before the migration row, or migrating forward again
// would see a schema it did not write.
func rewindCertificates(t *testing.T, database *sql.DB) {
	t.Helper()
	rewindNodePublicAddresses(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0137_certificates.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DROP INDEX inbounds_certificate",
		"ALTER TABLE inbounds DROP COLUMN certificate_id",
		"DROP TABLE certificates",
		"DELETE FROM schema_migrations WHERE version='0137_certificates.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

package recovery

import (
	"database/sql"
	"testing"
)

func rewindTunnelPublication(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0149_tunnel_publication_finalization.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"ALTER TABLE deployment_batches DROP COLUMN tunnel_publication_json",
		"ALTER TABLE deployment_batches DROP COLUMN tunnel_finalized",
		"DELETE FROM schema_migrations WHERE version='0149_tunnel_publication_finalization.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

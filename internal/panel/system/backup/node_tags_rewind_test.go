package backup

import (
	"database/sql"
	"testing"
)

// rewindNodeTags removes migration 0144 (node_meta.tags_json) so a fixture can
// stand in for an archive written before it.
func rewindNodeTags(t *testing.T, database *sql.DB) {
	t.Helper()
	rewindOutboundSource(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0144_node_tags.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"ALTER TABLE node_meta DROP COLUMN tags_json",
		"DELETE FROM schema_migrations WHERE version='0144_node_tags.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

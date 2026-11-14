package recovery

import (
	"database/sql"
	"testing"
)

// rewindPost0143 removes migrations 0146 (canvas entry bindings), 0145
// (outbound source) and 0144 (node tags), newest first, so a fixture can stand in for an
// archive written before them.
func rewindPost0143(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, step := range []struct {
		version string
		queries []string
	}{
		{"0148_canvas_entry_baseline.sql", []string{"ALTER TABLE canvas_entry_bindings DROP COLUMN creation_config_json", "ALTER TABLE canvas_entry_bindings DROP COLUMN creation_groups_json"}},
		{"0147_imported_proxy_identity.sql", []string{"DROP TABLE import_identity_reconcile", "DROP TABLE import_identity_map", "ALTER TABLE imported_proxies DROP COLUMN revision"}},
		{"0146_canvas_entry_bindings.sql", []string{"DROP TABLE canvas_entry_bindings"}},
		{"0145_outbound_source.sql", []string{"ALTER TABLE outbounds DROP COLUMN source_ip", "ALTER TABLE outbounds DROP COLUMN source_interface"}},
		{"0144_node_tags.sql", []string{"ALTER TABLE node_meta DROP COLUMN tags_json"}},
	} {
		var applied int
		if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version=?", step.version).Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied == 0 {
			continue
		}
		for _, query := range append(step.queries, "DELETE FROM schema_migrations WHERE version='"+step.version+"'") {
			if _, err := database.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
	}
}

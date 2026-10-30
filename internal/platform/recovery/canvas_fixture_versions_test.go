package recovery

import (
	"database/sql"
	"testing"
)

// Rewind only a temporary fixture's concurrent post-K4 migration. Production
// migration files and real databases are never changed by this helper.
func rewindPostK4Fixture(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0125_multi_core_panel.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DROP VIEW node_traffic_samples", "DROP TABLE core_traffic_samples", "DROP TABLE node_traffic_cursors", "DROP TABLE core_remove_tasks",
		"ALTER TABLE node_restart_requests DROP COLUMN core", "ALTER TABLE node_health_checks DROP COLUMN core",
		"ALTER TABLE nodes DROP COLUMN node_traffic_supported", "ALTER TABLE nodes DROP COLUMN multi_core_supported", "ALTER TABLE inbounds DROP COLUMN core",
		"DROP INDEX core_update_active_component", "ALTER TABLE core_update_tasks DROP COLUMN adding",
		"CREATE UNIQUE INDEX core_update_active_node ON core_update_tasks(node_id) WHERE status IN ('queued','running')",
		"CREATE UNIQUE INDEX inbound_active_port ON inbounds(node_id,port) WHERE enabled=1",
		"DELETE FROM schema_migrations WHERE version='0125_multi_core_panel.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

package backup

import (
	"database/sql"
	"strings"
	"testing"

	"proxycontrol/migrations"
)

// rewindTunnelImportTarget restores the 0151 table, including rule memberships,
// before an older archive fixture is built.
func rewindTunnelImportTarget(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0152_tunnel_rule_import_target.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	base, err := migrations.Files.ReadFile("0129_tunnels.sql")
	if err != nil {
		t.Fatal(err)
	}
	original := string(base)
	start := strings.Index(original, "CREATE TABLE tunnel_rules (")
	end := strings.Index(original[start:], "CREATE INDEX tunnel_rules_tunnel") + start
	table := strings.Replace(original[start:end], "CREATE TABLE tunnel_rules (", "CREATE TABLE tunnel_rules_old (", 1)
	table = strings.Replace(table, "    CHECK((target_mode=", "    published_snapshot TEXT,\n    CHECK((target_mode=", 1)
	columns := "id,name,entry_node_id,listen_port,port_mode,network,tunnel_id,target_mode,target_inbound_id,target_host,target_port,owner_node_id,subscription_visible,enabled,status,draft_version,published_version,published_at,created_at,updated_at,published_snapshot"
	triggers := original[strings.Index(original, "CREATE TRIGGER tunnel_rule_node_address_changed"):]
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, q := range []string{
		"CREATE TEMP TABLE saved_rule_groups AS SELECT * FROM node_group_relays",
		"DROP TRIGGER tunnel_rule_node_address_changed",
		"DROP TRIGGER tunnel_rule_inbound_port_changed",
		table,
		"INSERT INTO tunnel_rules_old(" + columns + ") SELECT " + columns + " FROM tunnel_rules",
		"DROP TABLE tunnel_rules",
		"ALTER TABLE tunnel_rules_old RENAME TO tunnel_rules",
		"CREATE INDEX tunnel_rules_tunnel ON tunnel_rules(tunnel_id)",
		"CREATE INDEX tunnel_rules_target ON tunnel_rules(target_inbound_id)",
		triggers,
		"INSERT INTO node_group_relays SELECT * FROM saved_rule_groups",
		"DROP TABLE saved_rule_groups",
		"DELETE FROM schema_migrations WHERE version='0152_tunnel_rule_import_target.sql'",
	} {
		if _, err := tx.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

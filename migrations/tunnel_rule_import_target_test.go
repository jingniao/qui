package migrations

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestTunnelRuleImportTargetMigrationPreservesPublishedRules(t *testing.T) {
	database := tunnelRulesAt0151(t)
	const columns = `json_array(id,name,entry_node_id,listen_port,port_mode,network,tunnel_id,target_mode,target_inbound_id,target_host,target_port,owner_node_id,subscription_visible,enabled,status,draft_version,published_version,published_at,created_at,updated_at,published_snapshot)`
	before := importMigrationStrings(t, database, "SELECT "+columns+" FROM tunnel_rules ORDER BY id")
	members := importMigrationStrings(t, database, "SELECT group_id||'/'||rule_id FROM node_group_relays ORDER BY 1")
	const oldTriggers = "SELECT sql FROM sqlite_schema WHERE type='trigger' AND name IN ('tunnel_rule_node_address_changed','tunnel_rule_inbound_port_changed') ORDER BY name"
	triggers := importMigrationStrings(t, database, oldTriggers)
	if _, err := database.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	applyWhole(t, database, "0152_tunnel_rule_import_target.sql")
	if _, err := database.Exec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	if got := importMigrationStrings(t, database, "SELECT "+columns+" FROM tunnel_rules ORDER BY id"); !reflect.DeepEqual(got, before) {
		t.Fatalf("rule values changed:\nbefore %v\nafter %v", before, got)
	}
	if got := importMigrationStrings(t, database, "SELECT group_id||'/'||rule_id FROM node_group_relays ORDER BY 1"); !reflect.DeepEqual(got, members) {
		t.Fatalf("members changed: %v -> %v", members, got)
	}
	if got := importMigrationStrings(t, database, oldTriggers); !reflect.DeepEqual(got, triggers) || len(got) != 2 {
		t.Fatalf("triggers changed: %v -> %v", triggers, got)
	}
	for _, name := range []string{"tunnel_rules_tunnel", "tunnel_rules_target", "tunnel_rules_import_target", "node_group_relays_rule"} {
		var count int
		if err := database.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='index' AND name=?", name).Scan(&count); err != nil || count != 1 {
			t.Fatal(name, count, err)
		}
	}
	for _, query := range []string{"UPDATE nodes SET public_ip='new.example' WHERE id='target'", "UPDATE inbounds SET port=8443 WHERE id='inbound'"} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
		var status, stamp string
		var version int
		if err := database.QueryRow("SELECT status,draft_version,updated_at FROM tunnel_rules WHERE id='linked'").Scan(&status, &version, &stamp); err != nil || status != "needs_publish" || version != 8 || stamp == "updated" {
			t.Fatal(status, version, stamp, err)
		}
		if _, err := database.Exec("UPDATE tunnel_rules SET status='published',draft_version=7,updated_at='updated' WHERE id='linked'"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := database.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation after migration")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestTunnelRuleImportTargetConstraints(t *testing.T) {
	database := tunnelRulesAt0151(t)
	applyWhole(t, database, "0152_tunnel_rule_import_target.sql")
	const insert = `INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,target_mode,target_inbound_id,target_host,target_port,target_import_id,subscription_visible,enabled,status,created_at,updated_at) VALUES('import','import','entry',30003,'manual','both',?,?,?,?,?,1,1,'draft','created','updated')`
	if _, err := database.Exec(insert, "import", nil, nil, nil, "imported"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE tunnel_rules SET status='published',published_at='published' WHERE id='import'"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DELETE FROM imported_proxies WHERE id='imported'"); err != nil {
		t.Fatal(err)
	}
	var target sql.NullString
	if err := database.QueryRow("SELECT target_import_id FROM tunnel_rules WHERE id='import'").Scan(&target); err != nil || target.Valid {
		t.Fatal(target, err)
	}
	var status, stamp string
	var version int
	if err := database.QueryRow("SELECT status,draft_version,updated_at FROM tunnel_rules WHERE id='import'").Scan(&status, &version, &stamp); err != nil || status != "needs_publish" || version != 2 || stamp == "updated" {
		t.Fatal(status, version, stamp, err)
	}
	if _, err := database.Exec("DELETE FROM tunnel_rules WHERE id='import'"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(insert, "import", nil, nil, nil, nil); err != nil {
		t.Fatal("deleted import target must remain representable", err)
	}
	for _, query := range []string{
		"UPDATE tunnel_rules SET target_import_id='missing' WHERE id='import'",
		"UPDATE tunnel_rules SET target_inbound_id='inbound' WHERE id='import'",
		"UPDATE tunnel_rules SET target_host='example.org' WHERE id='import'",
		"UPDATE tunnel_rules SET target_port=443 WHERE id='import'",
		"UPDATE tunnel_rules SET target_mode='unknown' WHERE id='import'",
		"UPDATE tunnel_rules SET subscription_visible=1 WHERE id='manual'",
		"UPDATE tunnel_rules SET target_inbound_id=NULL WHERE id='linked'",
		"UPDATE tunnel_rules SET target_host='example.org' WHERE id='linked'",
	} {
		if _, err := database.Exec(query); err == nil {
			t.Fatalf("accepted invalid target: %s", query)
		}
	}
	if _, err := database.Exec("INSERT INTO imported_proxies VALUES('other')"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"linked", "manual"} {
		if _, err := database.Exec("UPDATE tunnel_rules SET target_import_id='other' WHERE id=?", mode); err == nil {
			t.Fatalf("accepted an import target on %s", mode)
		}
	}
	for _, state := range []string{"draft", "pending_delete"} {
		if _, err := database.Exec("UPDATE tunnel_rules SET target_import_id='other',status=?,published_at=NULL,draft_version=1,updated_at='unchanged' WHERE id='import'", state); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec("DELETE FROM imported_proxies WHERE id='other'"); err != nil {
			t.Fatal(err)
		}
		wantVersion := 2
		if state == "pending_delete" {
			wantVersion = 1
		}
		if err := database.QueryRow("SELECT status,draft_version FROM tunnel_rules WHERE id='import'").Scan(&status, &version); err != nil || status != state || version != wantVersion {
			t.Fatal(status, version, err)
		}
		if _, err := database.Exec("INSERT INTO imported_proxies VALUES('other')"); err != nil {
			t.Fatal(err)
		}
	}
}

func tunnelRulesAt0151(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	for _, query := range []string{
		"PRAGMA foreign_keys=ON",
		"CREATE TABLE nodes(id TEXT PRIMARY KEY,public_ip TEXT)",
		"CREATE TABLE inbounds(id TEXT PRIMARY KEY,node_id TEXT REFERENCES nodes(id),port INTEGER)",
		"CREATE TABLE imported_proxies(id TEXT PRIMARY KEY)",
		"CREATE TABLE node_groups(id TEXT PRIMARY KEY)",
		"CREATE TABLE node_group_inbounds(group_id TEXT REFERENCES node_groups(id),inbound_id TEXT REFERENCES inbounds(id))",
		"INSERT INTO nodes VALUES('entry','entry.example'),('target','target.example')",
		"INSERT INTO inbounds VALUES('inbound','target',443)",
		"INSERT INTO imported_proxies VALUES('imported')",
		"INSERT INTO node_groups VALUES('g1'),('g2')",
		"INSERT INTO node_group_inbounds VALUES('g1','inbound'),('g2','inbound')",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(query, err)
		}
	}
	applyWhole(t, database, "0129_tunnels.sql")
	applyWhole(t, database, "0130_relay_subscription_snapshot.sql")
	for _, query := range []string{
		"INSERT INTO tunnels(id,name,transport,status,gost_material,created_at,updated_at) VALUES('tunnel','tunnel','tls','published',x'01','created','updated')",
		`INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,tunnel_id,target_mode,target_inbound_id,target_host,target_port,owner_node_id,subscription_visible,enabled,status,draft_version,published_version,published_at,created_at,updated_at,published_snapshot) VALUES('linked','linked name','entry',30001,'auto','both','tunnel','linked','inbound',NULL,NULL,'target',1,1,'published',7,6,'published','created','updated','{"id":"linked","port":30001}')`,
		`INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,target_mode,target_host,target_port,subscription_visible,enabled,status,draft_version,published_version,published_at,created_at,updated_at,published_snapshot) VALUES('manual','manual name','entry',30002,'manual','tcp','manual','manual.example',8443,0,0,'modified',9,4,'published manual','created manual','updated manual','{"id":"manual","port":30002}')`,
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(query, err)
		}
	}
	applyWhole(t, database, "0151_node_group_relays.sql")
	if _, err := database.Exec("INSERT INTO node_group_relays VALUES('g2','manual')"); err != nil {
		t.Fatal(err)
	}
	return database
}

func importMigrationStrings(t *testing.T, database *sql.DB, query string) []string {
	t.Helper()
	rows, err := database.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

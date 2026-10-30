package migrations

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func TestMultiCorePreservesBackfilledComponentsAndSeparatesSlots(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		"CREATE TABLE nodes(id TEXT PRIMARY KEY)", "CREATE TABLE config_revisions(id INTEGER PRIMARY KEY)",
		"CREATE TABLE inbounds(id TEXT PRIMARY KEY,node_id TEXT,port INTEGER,enabled INTEGER)", "CREATE UNIQUE INDEX inbound_active_port ON inbounds(node_id,port) WHERE enabled=1",
		"CREATE TABLE node_health_checks(id TEXT)", "CREATE TABLE node_restart_requests(command_id TEXT)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	apply(t, db, "0035_core_updates.sql")
	apply(t, db, "0107_core_update_direct_url.sql")
	insert := `INSERT INTO core_update_tasks(id,node_id,baseline_revision,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at) VALUES(?, 'n',1,?,'1.0.0','',printf('%064d',0),1,'queued',?,'owner','now','now')`
	if _, err := db.Exec(insert, "old", "sing-box", "old"); err != nil {
		t.Fatal(err)
	}
	apply(t, db, "0125_multi_core_panel.sql")
	var component string
	if err := db.QueryRow("SELECT component FROM core_update_tasks WHERE id='old'").Scan(&component); err != nil || component != "sing-box" {
		t.Fatal(component, err)
	}
	if _, err := db.Exec(insert, "peer", "xray", "peer"); err != nil {
		t.Fatal("independent component blocked", err)
	}
	if _, err := db.Exec(insert, "duplicate", "sing-box", "duplicate"); err == nil {
		t.Fatal("same component acquired two active slots")
	}
}

package migrations_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"

	_ "modernc.org/sqlite"
)

// preGostSchema is the embedded migration set without 0127, so the runner
// upgrades a real 0126 deployment instead of a hand-written fixture.
func preGostSchema(t *testing.T) fstest.MapFS {
	t.Helper()
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	subset := fstest.MapFS{}
	for _, name := range names {
		if name == "0127_gost_core.sql" {
			continue
		}
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		subset[name] = &fstest.MapFile{Data: data}
	}
	return subset
}

// TestGostCoreMigrationKeepsDeployedData upgrades a populated 0126 database to
// 0127. GOST is a third core name in five tables, four of which are rebuilt, so
// the test proves the rows, keys, indexes, triggers, view and foreign keys all
// survive, that 'gost' is accepted where a core name is stored and that every
// other value is still refused.
func TestGostCoreMigrationKeepsDeployedData(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if _, err := database.ExecContext(ctx, "PRAGMA foreign_keys(1)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, database, preGostSchema(t)); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"INSERT INTO nodes(id,name,status,created_at) VALUES('n1','Node 1','online','2026-01-01T00:00:00Z')",
		"INSERT INTO nodes(id,name,status,created_at) VALUES('n2','Node 2','online','2026-01-01T00:00:00Z')",
		"INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(1,'owner','seed','success','2026-01-01T00:00:00Z',x'00')",
		"INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,relay_credential_id,created_at,core) VALUES('in1','entry','n1','vless','0.0.0.0',443,1,1,0,'direct','[]',x'00','','2026-01-01T00:00:00Z','sing-box')",
		"INSERT INTO core_update_tasks(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json,adding) VALUES('task-1','n1',1,NULL,'sing-box','1.14.3','',printf('%064d',0),1,'queued','upd_old','owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',NULL,1)",
		"INSERT INTO core_remove_tasks(id,node_id,component,revision,status,code,result_json,created_by,created_at,updated_at) VALUES('rm-1','n2','xray',1,'queued','',NULL,'owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')",
		"INSERT INTO node_traffic_cursors(node_id,epoch,credential_id,core,revision,upload_bytes,download_bytes,observed_at) VALUES('n1','epoch-1.node','node','xray',1,10,20,'2026-01-01T00:00:00Z')",
		"INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES('n1','xray','epoch-1.node','2026-01-01T00:00:00Z',10,20)",
		"INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES('n1','epoch-1','c1','2026-01-01T00:00:00Z','u1','d1',1,2)",
		"INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES('n1','epoch-2','c1','2026-01-01T00:00:00Z','u1','d1',3,4)",
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatalf("0127 did not apply: %v", err)
	}
	// Every row keeps its identity, its values and its state.
	for _, check := range []struct {
		query string
		want  int
	}{
		{"SELECT count(*) FROM inbounds WHERE id='in1' AND name='entry' AND protocol='vless' AND port=443 AND core='sing-box'", 1},
		{"SELECT count(*) FROM core_update_tasks WHERE id='task-1' AND node_id='n1' AND component='sing-box' AND version='1.14.3' AND sequence=1 AND status='queued' AND command_id='upd_old' AND adding=1 AND result_json IS NULL", 1},
		{"SELECT count(*) FROM core_remove_tasks WHERE id='rm-1' AND node_id='n2' AND component='xray' AND status='queued' AND code='' AND result_json IS NULL", 1},
		{"SELECT count(*) FROM node_traffic_cursors WHERE node_id='n1' AND epoch='epoch-1.node' AND core='xray' AND revision=1 AND upload_bytes=10 AND download_bytes=20", 1},
		{"SELECT count(*) FROM core_traffic_samples WHERE node_id='n1' AND core='xray' AND observed_at='2026-01-01T00:00:00Z' AND download_bytes=20", 1},
		{"SELECT count(*) FROM nodes WHERE gost_core_supported=0", 2},
	} {
		var got int
		if err := database.QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s: %d %v", check.query, got, err)
		}
	}
	var notNull int
	var defaultValue string
	if err := database.QueryRowContext(ctx, "SELECT \"notnull\",dflt_value FROM pragma_table_info('inbounds') WHERE name='core'").Scan(&notNull, &defaultValue); err != nil || notNull != 1 || defaultValue != "''" {
		t.Fatalf("replacement core column: %d %q %v", notNull, defaultValue, err)
	}
	// The rebuild recreated every index it dropped together with its table.
	for _, name := range []string{"core_update_active_component", "core_remove_active_component", "core_traffic_time"} {
		var count int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", name).Scan(&count); err != nil || count != 1 {
			t.Fatalf("index %s: %d %v", name, count, err)
		}
	}
	// The column replacement is only safe because nothing else in the schema
	// reads inbounds.core; this asserts the fact the migration relies on.
	objects, err := database.QueryContext(ctx, "SELECT name,sql FROM sqlite_master WHERE type IN ('index','trigger','view') AND sql LIKE '%inbounds%'")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for objects.Next() {
		var name, definition string
		if err := objects.Scan(&name, &definition); err != nil {
			objects.Close()
			t.Fatal(err)
		}
		found++
		if strings.Contains(definition, "core") {
			objects.Close()
			t.Fatalf("%s reads inbounds.core: %s", name, definition)
		}
	}
	objects.Close()
	if found == 0 {
		t.Fatal("no index, trigger or view references inbounds, so the check proves nothing")
	}
	// The view keeps its account fallback: an epoch the node counter covers is
	// not counted twice, an epoch without a node cursor is.
	for _, check := range []struct {
		query string
		want  int
	}{
		{"SELECT count(*) FROM node_traffic_samples WHERE core='xray' AND epoch='epoch-1.node' AND download_bytes=20", 1},
		{"SELECT count(*) FROM node_traffic_samples WHERE core='' AND epoch IN ('epoch-1','epoch-2')", 1},
		{"SELECT count(*) FROM node_traffic_samples WHERE core='' AND epoch='epoch-2' AND upload_bytes=3", 1},
	} {
		var got int
		if err := database.QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s: %d %v", check.query, got, err)
		}
	}
	// GOST is a core name in every table that stores one.
	for _, statement := range []string{
		"UPDATE inbounds SET core='gost' WHERE id='in1'",
		"INSERT INTO core_update_tasks(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json,adding) VALUES('task-2','n2',1,NULL,'gost','3.3.2','',printf('%064d',0),1,'queued','upd_gost','owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',NULL,1)",
		"INSERT INTO core_remove_tasks(id,node_id,component,revision,status,code,result_json,created_by,created_at,updated_at) VALUES('rm-2','n1','gost',1,'success','success',NULL,'owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')",
		"INSERT INTO node_traffic_cursors(node_id,epoch,credential_id,core,revision,upload_bytes,download_bytes,observed_at) VALUES('n2','gost-1.node','node','gost',1,1,2,'2026-01-02T00:00:00Z')",
		"INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES('n2','gost','gost-1.node','2026-01-02T00:00:00Z',1,2)",
		"UPDATE nodes SET gost_core_supported=1 WHERE id='n2'",
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("rejected: %s: %v", statement, err)
		}
	}
	// Every other value is still refused by the widened CHECKs, and the active
	// slot of both task tables still holds one task per (node, component).
	for _, statement := range []string{
		"UPDATE inbounds SET core='vmess' WHERE id='in1'",
		"UPDATE inbounds SET core='GOST' WHERE id='in1'",
		"INSERT INTO core_update_tasks(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json,adding) VALUES('task-3','n1',1,NULL,'nginx','1.0.0','',printf('%064d',0),1,'queued','upd_bad','owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',NULL,0)",
		"INSERT INTO core_update_tasks(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json,adding) VALUES('task-4','n2',1,NULL,'gost','3.3.2','',printf('%064d',0),2,'queued','upd_duplicate','owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',NULL,1)",
		"INSERT INTO core_remove_tasks(id,node_id,component,revision,status,code,result_json,created_by,created_at,updated_at) VALUES('rm-3','n2','xray',1,'queued','',NULL,'owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')",
		"UPDATE core_remove_tasks SET component='nginx' WHERE id='rm-1'",
		"UPDATE node_traffic_cursors SET core='gost2' WHERE epoch='epoch-1.node'",
		"UPDATE core_traffic_samples SET core='' WHERE epoch='epoch-1.node'",
		"UPDATE nodes SET gost_core_supported=2 WHERE id='n1'",
	} {
		if _, err := database.ExecContext(ctx, statement); err == nil {
			t.Fatalf("accepted: %s", statement)
		}
	}
	// A queued removal of one component does not consume another component's
	// update slot.
	if _, err := database.ExecContext(ctx, "INSERT INTO core_update_tasks(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json,adding) VALUES('task-5','n2',1,NULL,'xray','26.3.27','',printf('%064d',0),1,'queued','upd_xray','owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',NULL,0)"); err != nil {
		t.Fatalf("independent component blocked: %v", err)
	}
	// Foreign keys and page integrity survive the transactional rebuild.
	fkRows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if fkRows.Next() {
		var table string
		fkRows.Scan(&table)
		fkRows.Close()
		t.Fatalf("foreign key violation in %s", table)
	}
	fkRows.Close()
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %q %v", integrity, err)
	}
	// Running the migrations again changes nothing.
	if err := db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	var tasks int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM core_update_tasks").Scan(&tasks); err != nil || tasks != 3 {
		t.Fatalf("rerun disturbed the tasks: %d %v", tasks, err)
	}
}

package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"proxycontrol/migrations"
)

func TestReinstallMigrationPreservesUpdateEvidence(t *testing.T) {
	ctx := context.Background()
	uri, err := URI(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if err := Migrate(ctx, database, migrationFilesThrough(t, "0164_traffic_hour_sample_counts.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
INSERT INTO nodes(id,name,status,created_at) VALUES('n','node','offline','before');
INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(1,'owner','baseline','success','before',x'010203');
INSERT INTO release_catalogue VALUES('release','proxy-agent','linux','amd64',1,'2.0.0',printf('%064d',0),x'010203','key','owner','before');
INSERT INTO agent_system_update_tasks VALUES('system','n',1,'2.0.0','unconfirmed',1,'attempt','owner','before','before','agent_install',x'010203','dispatch failure');
INSERT INTO agent_system_update_attempts VALUES('attempt','system',1,x'000102',x'030405',x'010203',1,3,'before',x'060708');
INSERT INTO agent_system_update_logs VALUES('attempt',1,0,printf('%064d',0),x'090a0b');
INSERT INTO agent_update_tasks VALUES('worker','n','release',1,'running',1,'worker-command','worker-command','owner','before','before',NULL);
INSERT INTO agent_update_attempts VALUES('worker-command','worker',1,'install',x'0c0d0e',NULL,'before');
INSERT INTO core_update_tasks VALUES('core','n',1,NULL,'xray','26.3.27','https://example.test/core',printf('%064d',0),1,'running','core-command','owner','before','before',x'0f1011',1);`); err != nil {
		t.Fatal(err)
	}
	tables := []string{"agent_system_update_tasks", "agent_system_update_attempts", "agent_system_update_logs", "agent_update_tasks", "agent_update_attempts", "core_update_tasks"}
	snapshot := func(table string) string {
		t.Helper()
		rows, err := database.Query("SELECT * FROM " + table + " ORDER BY 1")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		values := [][]any{}
		for rows.Next() {
			row := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range row {
				ptrs[i] = &row[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			values = append(values, row)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	before := map[string]string{}
	for _, table := range tables {
		before[table] = snapshot(table)
	}
	if err := Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if got := snapshot(table); got != before[table] {
			t.Fatalf("migration changed %s: %s -> %s", table, before[table], got)
		}
	}
	for _, table := range []string{"agent_system_update_tasks", "agent_update_tasks", "core_update_tasks"} {
		if _, err := database.Exec("UPDATE " + table + " SET status='superseded'"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO agent_system_update_tasks SELECT 'next-system',node_id,baseline_revision,target_version,'queued',1,'next-attempt',created_by,created_at,updated_at,'',NULL,'' FROM agent_system_update_tasks;
INSERT INTO agent_update_tasks SELECT 'next-worker',node_id,release_id,baseline_revision,'queued',1,'next-worker-command','next-worker-command',created_by,created_at,updated_at,NULL FROM agent_update_tasks;
INSERT INTO core_update_tasks SELECT 'next-core',node_id,baseline_revision,release_id,component,version,url,sha256,sequence,'queued','next-core-command',created_by,created_at,updated_at,NULL,adding FROM core_update_tasks;`); err != nil {
		t.Fatal("superseded task blocks new active task", err)
	}
	for _, query := range []string{
		"INSERT INTO agent_system_update_tasks SELECT 'duplicate-system',node_id,baseline_revision,target_version,'queued',1,'duplicate',created_by,created_at,updated_at,'',NULL,'' FROM agent_system_update_tasks WHERE id='next-system'",
		"INSERT INTO agent_update_tasks SELECT 'duplicate-worker',node_id,release_id,baseline_revision,'queued',1,'duplicate-worker','duplicate-worker',created_by,created_at,updated_at,NULL FROM agent_update_tasks WHERE id='next-worker'",
		"INSERT INTO core_update_tasks SELECT 'duplicate-core',node_id,baseline_revision,release_id,component,version,url,sha256,sequence,'queued','duplicate-core',created_by,created_at,updated_at,NULL,adding FROM core_update_tasks WHERE id='next-core'",
	} {
		if _, err := database.Exec(query); err == nil {
			t.Fatal("active unique index lost")
		}
	}
	rows, err := database.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration broke foreign keys")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

package migrations

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestCoreUpdateDirectUrlBackfillMigratesHistoricalTasks runs the previous and
// the new core-update schema against a real database and checks that historical
// tasks survive with an empty direct URL and no attempt table.
func TestCoreUpdateDirectUrlBackfillMigratesHistoricalTasks(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	digest := strings.Repeat("ab", 32)
	for _, statement := range []string{
		`CREATE TABLE config_revisions(id INTEGER PRIMARY KEY)`,
		`CREATE TABLE nodes(id TEXT PRIMARY KEY)`,
		`INSERT INTO nodes(id) VALUES('node-1')`,
		`INSERT INTO config_revisions(id) VALUES(4)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	apply(t, database, "0035_core_updates.sql")
	if _, err := database.Exec("INSERT INTO release_catalogue(id,component,operating_system,architecture,release_sequence,version,digest,signed_json,signing_public_key,created_by,created_at) VALUES('rel-1','xray','linux','amd64',7,'26.3.27',?,'{}','key','owner','2026-01-01T00:00:00Z')", digest); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO core_update_tasks(id,node_id,release_id,baseline_revision,status,generation,command_id,original_command_id,created_by,created_at,updated_at) VALUES('task-1','node-1','rel-1',4,'failed_closed',1,'upd_old','upd_old','owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO core_update_attempts(command_id,task_id,generation,operation,request_json,created_at) VALUES('upd_old','task-1',1,'install','{}','2026-01-01T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	apply(t, database, "0107_core_update_direct_url.sql")
	var component, version, url, sha, status, commandID string
	var sequence, revision int64
	var releaseID sql.NullString
	if err := database.QueryRow("SELECT component,version,url,sha256,sequence,status,command_id,baseline_revision,release_id FROM core_update_tasks WHERE id='task-1'").Scan(&component, &version, &url, &sha, &sequence, &status, &commandID, &revision, &releaseID); err != nil {
		t.Fatal(err)
	}
	if component != "xray" || version != "26.3.27" || url != "" || sha != digest || sequence != 7 || status != "cleared" || commandID != "upd_old" || revision != 4 || releaseID.String != "rel-1" {
		t.Fatalf("unexpected backfilled row: %q %q %q %q %d %q %q %d %q", component, version, url, sha, sequence, status, commandID, revision, releaseID.String)
	}
	var attempts int
	if err := database.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='core_update_attempts'").Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("attempt table survived migration: %d %v", attempts, err)
	}
}

func apply(t *testing.T, database *sql.DB, name string) {
	t.Helper()
	data, err := Files.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(data), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

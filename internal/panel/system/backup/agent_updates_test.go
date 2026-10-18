package backup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"proxycontrol/pkg/contracts/release"
	"strings"
	"testing"
	"time"
)

func TestAgentArchiveInventoryAndConflictingUpdates(t *testing.T) {
	source, _, _ := sourceFixture(t)
	database := source.Database
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	var nodeID string
	var revision int64
	if err := database.QueryRow("SELECT n.id,r.id FROM nodes n CROSS JOIN config_revisions r LIMIT 1").Scan(&nodeID, &revision); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(?,?,'success',?)", revision, nodeID, time.Now().UTC().Format(time.RFC3339Nano))
	before, err := ReadInventory(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	signed, err := release.Sign(release.Manifest{SchemaVersion: 1, Component: "proxy-agent", OS: "linux", Architecture: "amd64", Version: "2.0.0", Sequence: 1, Bytes: 100, SHA256: strings.Repeat("a", 64), IssuedAt: at, ExpiresAt: at.Add(time.Hour)}, private)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(signed)
	exec("INSERT INTO release_catalogue VALUES('agent-release','proxy-agent','linux','amd64',1,'2.0.0',?,?,?,'owner',?)", signed.Manifest.SHA256, data, base64.StdEncoding.EncodeToString(public), at.Format(time.RFC3339Nano))
	request := release.AgentUpdateRequest{SchemaVersion: 1, ID: "aup_test", NodeID: nodeID, Revision: revision, Operation: "install", Release: signed}
	data, _ = json.Marshal(request)
	exec("INSERT INTO agent_update_tasks VALUES('agent-task',?,'agent-release',?,'queued',1,'aup_test','aup_test','owner',?,?,NULL)", nodeID, revision, at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano))
	exec("INSERT INTO agent_update_attempts VALUES('aup_test','agent-task',1,'install',?,NULL,?)", data, at.Format(time.RFC3339Nano))
	if err := validateAgentUpdates(ctx, database); err != nil {
		t.Fatal("valid pending Agent archive rejected", err)
	}
	for _, status := range []string{"queued", "running", "failed_closed", "success", "rejected", "rolled_back"} {
		exec("UPDATE agent_update_tasks SET status=?", status)
		inventory, err := ReadInventory(ctx, database)
		if err != nil {
			t.Fatal(err)
		}
		want := before.Totals.PendingOperations
		if status == "queued" || status == "running" || status == "failed_closed" {
			want++
		}
		if inventory.Totals.PendingOperations != want {
			t.Fatalf("%s: got %d pending, want %d", status, inventory.Totals.PendingOperations, want)
		}
	}
	exec("UPDATE agent_update_tasks SET status='running'")
	exec("INSERT INTO core_update_tasks(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at) SELECT 'core-task',node_id,baseline_revision,release_id,'xray','26.3.27','',printf('%064d',0),1,'running','core-command',created_by,created_at,updated_at FROM agent_update_tasks")
	if err := validateAgentUpdates(ctx, database); err == nil {
		t.Fatal("simultaneous active updates accepted")
	}
	exec("DELETE FROM core_update_tasks")
	if err := validateAgentUpdates(ctx, database); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO node_update_capabilities(node_id,core_supported,agent_supported) VALUES(?,0,1)", nodeID)
	exec("PRAGMA ignore_check_constraints=ON")
	exec("UPDATE node_update_capabilities SET agent_supported=2")
	if err := validateAgentUpdates(ctx, database); err == nil {
		t.Fatal("invalid Agent capability accepted")
	}
}

func TestAgentArchiveLegacyAndMissingTables(t *testing.T) {
	source, _, _ := sourceFixture(t)
	database := source.Database
	ctx := context.Background()
	for _, query := range []string{"DROP TABLE agent_update_attempts", "DROP TABLE agent_update_tasks"} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
		if err := validateAgentUpdates(ctx, database); err == nil {
			t.Fatal("missing migrated tables accepted")
		}
	}
	if _, err := database.Exec("DELETE FROM schema_migrations WHERE version IN ('0036_agent_updates.sql','0037_agent_recovery.sql')"); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentUpdates(ctx, database); err != nil {
		t.Fatal("legacy archive rejected", err)
	}
	if _, err := ReadInventory(ctx, database); err != nil {
		t.Fatal("legacy inventory rejected", err)
	}
}

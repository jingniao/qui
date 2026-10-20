package deployment

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
	"time"
)

func closeTasksFixture(t *testing.T) (*Repository, *node.Repository, string, Revision) {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Deleting node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := &Repository{Database: database, Vault: key}
	state := desiredstate.State{SchemaVersion: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	revision, err := r.Create(ctx, state, "owner", "pending")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	return r, nodes, n.ID, revision
}

func exec(t *testing.T, r *Repository, ctx context.Context, query string, args ...any) {
	t.Helper()
	if _, err := r.Database.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}

func text(t *testing.T, r *Repository, ctx context.Context, query string, args ...any) string {
	t.Helper()
	var value string
	if err := r.Database.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

// Deleting a node closes its own single-node work: the pending deployment, the
// queued updates and the pending health check. An active multi-node batch is not
// touched, because it blocks the deletion instead.
func TestCloseNodeTasksClosesSingleNodeWork(t *testing.T) {
	ctx := context.Background()
	r, _, nodeID, revision := closeTasksFixture(t)
	digest := "0000000000000000000000000000000000000000000000000000000000000000"
	exec(t, r, ctx, "INSERT INTO deployment_commands(id,node_id,revision,operation,status,created_at) VALUES('cmd-rollback',?,?,'rollback','pending','x')", nodeID, revision.ID)
	exec(t, r, ctx, "INSERT INTO core_update_tasks(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at) VALUES('core1',?,?,NULL,'xray','26.3.27','https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip',?,1,'queued','core-cmd','owner','x','x')", nodeID, revision.ID, digest)
	exec(t, r, ctx, "INSERT INTO release_catalogue(id,component,operating_system,architecture,release_sequence,version,digest,signed_json,signing_public_key,created_by,created_at) VALUES('rel1','proxy-agent','linux','amd64',1,'1.0.0',?,x'00','key','owner','x')", digest)
	exec(t, r, ctx, "INSERT INTO agent_update_tasks(id,node_id,release_id,baseline_revision,status,generation,command_id,original_command_id,created_by,created_at,updated_at) VALUES('agent1',?,'rel1',?,'queued',1,'agent-cmd','agent-cmd','owner','x','x')", nodeID, revision.ID)
	exec(t, r, ctx, "INSERT INTO agent_update_attempts(command_id,task_id,generation,operation,request_json,created_at) VALUES('agent-cmd','agent1',1,'install',x'00','x')")
	exec(t, r, ctx, "INSERT INTO node_health_checks(id,node_id,revision,status,created_at,deadline,created_by) VALUES('health1',?,?,'queued','x','y','owner')", nodeID, revision.ID)
	exec(t, r, ctx, "INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at) VALUES('batch1','chain','Batch','owner','deploying','x')")
	exec(t, r, ctx, "INSERT INTO deployment_batch_items(batch_id,position,node_id,revision,status) VALUES('batch1',0,?,?,'waiting')", nodeID, revision.ID)
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := r.CloseNodeTasks(ctx, tx, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if status, code := text(t, r, ctx, "SELECT status FROM deployment_targets WHERE node_id=? AND revision=?", nodeID, revision.ID), text(t, r, ctx, "SELECT result_code FROM deployment_targets WHERE node_id=? AND revision=?", nodeID, revision.ID); status != "failed" || code != "node_deleted" {
		t.Fatalf("pending target survived: %s %s", status, code)
	}
	if status := text(t, r, ctx, "SELECT status FROM config_revisions WHERE id=?", revision.ID); status != "failed" {
		t.Fatalf("revision is still %s", status)
	}
	if status := text(t, r, ctx, "SELECT status FROM deployment_commands WHERE id='cmd-rollback'"); status != "failed" {
		t.Fatalf("pending command is still %s", status)
	}
	if status := text(t, r, ctx, "SELECT status FROM core_update_tasks WHERE id='core1'"); status != "cleared" {
		t.Fatalf("core update task is still %s", status)
	}
	var agentTasks int
	if err := r.Database.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM agent_update_tasks WHERE node_id=?)+(SELECT count(*) FROM agent_update_attempts WHERE task_id='agent1')", nodeID).Scan(&agentTasks); err != nil || agentTasks != 0 {
		t.Fatalf("agent update task survived: %d %v", agentTasks, err)
	}
	if status := text(t, r, ctx, "SELECT status FROM node_health_checks WHERE id='health1'"); status != "expired" {
		t.Fatalf("health check is still %s", status)
	}
	if status, item := text(t, r, ctx, "SELECT status FROM deployment_batches WHERE id='batch1'"), text(t, r, ctx, "SELECT status FROM deployment_batch_items WHERE batch_id='batch1'"); status != "deploying" || item != "waiting" {
		t.Fatalf("active batch was closed: %s %s", status, item)
	}
}

// A revision another node still waits for stays pending.
func TestCloseNodeTasksKeepsSharedRevisionPending(t *testing.T) {
	ctx := context.Background()
	r, nodes, nodeID, revision := closeTasksFixture(t)
	other, _, _, err := nodes.CreateEnrollment(ctx, "Other node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, r, ctx, "INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(?,?,'pending','x')", revision.ID, other.ID)
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := r.CloseNodeTasks(ctx, tx, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if status := text(t, r, ctx, "SELECT status FROM config_revisions WHERE id=?", revision.ID); status != "pending" {
		t.Fatalf("shared revision is %s", status)
	}
}

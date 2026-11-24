package recovery

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/pkg/contracts/release"
)

func TestRestoreDoesNotReactivateSupersededUpdates(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	database := f.source.Database
	var revision int64
	if err := database.QueryRow("SELECT revision FROM node_runtime_state WHERE node_id=?", f.node.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	q := release.AgentSystemUpdateRequest{SchemaVersion: 1, Operation: "upgrade", TaskID: "old-system", AttemptID: "old-attempt", NodeID: f.node.ID, Revision: revision, TargetVersion: "2.0.0"}
	request, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO agent_system_update_tasks VALUES(?,?,?,'2.0.0','superseded',1,?,'owner',?,?,'agent_install',NULL,'')", []any{q.TaskID, f.node.ID, revision, q.AttemptID, at, at}},
		{"INSERT INTO agent_system_update_attempts(attempt_id,task_id,generation,request_json,created_at) VALUES(?,?,1,?,?)", []any{q.AttemptID, q.TaskID, request, at}},
		{"INSERT INTO core_update_tasks(id,node_id,baseline_revision,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at) VALUES('old-core',?,?,'xray','26.3.27','',?,1,'superseded','old-core-command','owner',?,?)", []any{f.node.ID, revision, strings.Repeat("a", 64), at, at}},
	} {
		if _, err := database.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal("completed identity replacement blocks restore", err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertText(t, restored, "SELECT status FROM agent_system_update_tasks WHERE id=?", "superseded", q.TaskID)
	assertText(t, restored, "SELECT status FROM core_update_tasks WHERE id='old-core'", "superseded")
	var got []byte
	if err := restored.QueryRow("SELECT request_json FROM agent_system_update_attempts WHERE attempt_id=?", q.AttemptID).Scan(&got); err != nil || string(got) != string(request) {
		t.Fatalf("restore changed old request: %s %v", got, err)
	}
	r := &deployment.Repository{Database: restored, Vault: f.source.ConfigurationVault}
	if command, err := r.PendingAgentSystemUpdate(ctx, f.node.ID); err != nil || command != nil {
		t.Fatalf("restore reactivated old system update: %v %v", command, err)
	}
	if command, err := r.PendingCoreUpdate(ctx, f.node.ID); err != nil || command != nil {
		t.Fatalf("restore reactivated old core update: %v %v", command, err)
	}
}

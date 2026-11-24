package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/platform/db"
	"proxycontrol/pkg/contracts/release"
)

func assertSupersededUpdatesArchive(t *testing.T, source Source, nodeID string, revision int64, digest string) {
	t.Helper()
	ctx := context.Background()
	database := source.Database
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	marshal := func(v any) []byte {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	baseline, err := ReadInventory(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	workerResult := marshal(release.UpdateResult{ID: "aup_test", NodeID: nodeID, Revision: revision, TargetDigest: digest, Status: "failed_closed", Code: "installation_failed"})
	exec("UPDATE agent_update_tasks SET status='superseded',result_json=? WHERE id='agent-task'", workerResult)
	exec("UPDATE agent_update_attempts SET result_json=? WHERE command_id='aup_test'", workerResult)
	q := release.AgentSystemUpdateRequest{SchemaVersion: 1, Operation: "upgrade", TaskID: "system-task", AttemptID: "system-attempt", NodeID: nodeID, Revision: revision, TargetVersion: "2.0.0"}
	failed := false
	result := marshal(release.AgentSystemUpdateResult{SchemaVersion: 1, TaskID: q.TaskID, AttemptID: q.AttemptID, NodeID: nodeID, Revision: revision, TargetVersion: q.TargetVersion, Status: "failed", Phase: "agent_install", Error: "original failure", StartedAt: at, FinishedAt: at, Recovery: release.AgentSystemUpdateRecovery{Attempted: true, Success: &failed}})
	content := []byte("original installer output\n")
	hash := sha256.Sum256(content)
	exec("INSERT INTO agent_system_update_tasks VALUES(?,?,?,'2.0.0','superseded',1,?,'owner',?,?,'agent_install',?,'')", q.TaskID, nodeID, revision, q.AttemptID, at, at, result)
	exec("INSERT INTO agent_system_update_attempts(attempt_id,task_id,generation,request_json,result_json,log_sequence,log_offset,created_at) VALUES(?,?,1,?,?,1,?,?)", q.AttemptID, q.TaskID, marshal(q), result, len(content), at)
	exec("INSERT INTO agent_system_update_logs VALUES(?,1,0,?,?)", q.AttemptID, hex.EncodeToString(hash[:]), content)
	coreResult := marshal(release.UpdateResult{ID: "core-command", NodeID: nodeID, Revision: revision, TargetDigest: digest, Status: "failed_closed", Code: "history_invalid"})
	exec("INSERT INTO core_update_tasks(id,node_id,baseline_revision,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json) VALUES('core-superseded',?,?,'xray','26.3.27','',?,1,'superseded','core-command','owner',?,?,?)", nodeID, revision, digest, at, at, coreResult)
	if err := validateAgentUpdates(ctx, database); err != nil {
		t.Fatal("superseded Agent results rejected", err)
	}
	if err := validateCoreUpdates(ctx, database); err != nil {
		t.Fatal("superseded core result rejected", err)
	}
	inventory, err := ReadInventory(ctx, database)
	if err != nil || inventory.Totals.PendingOperations != baseline.Totals.PendingOperations-1 {
		t.Fatalf("superseded work remains pending: %+v %v", inventory.Totals, err)
	}
	directory := t.TempDir()
	created, err := source.Create(ctx, directory, "superseded_updates", "owner", testPassword)
	if err != nil {
		t.Fatal("superseded updates cannot be backed up", err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, work string) error {
		snapshot, err := db.Open(ctx, filepath.Join(work, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer snapshot.Close()
		for table, want := range map[string][]byte{"agent_update_tasks": workerResult, "agent_system_update_tasks": result, "core_update_tasks": coreResult} {
			var status string
			var got []byte
			if err := snapshot.QueryRow("SELECT status,result_json FROM "+table).Scan(&status, &got); err != nil {
				return err
			}
			if status != "superseded" || !bytes.Equal(got, want) {
				return fmt.Errorf("%s lost superseded execution evidence", table)
			}
		}
		var got []byte
		if err := snapshot.QueryRow("SELECT content FROM agent_system_update_logs WHERE attempt_id=?", q.AttemptID).Scan(&got); err != nil {
			return err
		}
		if !bytes.Equal(got, content) {
			return fmt.Errorf("installer log changed")
		}
		return nil
	})
	if err != nil {
		t.Fatal("superseded archive rejected or changed", err)
	}
	// An unfinished task may have no result at all; that evidence also survives.
	exec("UPDATE agent_update_tasks SET result_json=NULL")
	exec("UPDATE agent_update_attempts SET result_json=NULL")
	exec("UPDATE agent_system_update_tasks SET result_json=NULL")
	exec("UPDATE agent_system_update_attempts SET result_json=NULL")
	exec("UPDATE core_update_tasks SET result_json=NULL")
	if err := validateAgentUpdates(ctx, database); err != nil {
		t.Fatal("superseded task without receipt rejected", err)
	}
	if err := validateCoreUpdates(ctx, database); err != nil {
		t.Fatal("superseded core without receipt rejected", err)
	}
}

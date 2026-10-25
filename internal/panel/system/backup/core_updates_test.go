package backup

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/release"
)

// TestClearedCoreUpdateKeepsStoredResultAndStillBacksUp covers the field-test
// row of 2026-10-25: a core update task the Owner cleared still holds the
// failed_closed result the Agent reported first. Inspection required
// result.Status == status and therefore refused the whole database, so that
// panel could never create another backup and no archive containing the row
// could be restored.
func TestClearedCoreUpdateKeepsStoredResultAndStillBacksUp(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	database := source.Database
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
	at := time.Now().UTC().Format(time.RFC3339Nano)
	digest := strings.Repeat("ab", 32)
	exec("INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(?,?,'success',?)", revision, nodeID, at)
	exec("INSERT INTO core_update_tasks(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at) VALUES('core-cleared',?,?,NULL,'xray','26.3.27','',?,1,'cleared','upd_cleared','owner',?,?)", nodeID, revision, digest, at, at)
	// The row the field test found: the Owner cleared a task that had already
	// stored the failed_closed result the Agent reported.
	failure, err := json.Marshal(release.UpdateResult{ID: "upd_cleared", NodeID: nodeID, Revision: revision, TargetDigest: digest, Status: "failed_closed", Code: "history_invalid"})
	if err != nil {
		t.Fatal(err)
	}
	exec("UPDATE core_update_tasks SET result_json=?", failure)
	// Create inspects every new archive, and a restore inspects it again.
	created, err := source.Create(ctx, directory, "cleared_core_backup_001", "owner", testPassword)
	if err != nil {
		t.Fatalf("backup of a database with a cleared core task failed: %v", err)
	}
	if _, err := Inspect(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword); err != nil {
		t.Fatalf("inspecting the new backup failed: %v", err)
	}
	// A cleared task may carry any terminal result the Agent can report; the
	// result was stored before the clear, so its status is not 'cleared'.
	for _, result := range []release.UpdateResult{
		{Status: "success", Code: "success", InstalledDigest: digest, InstalledVersion: "26.3.27", InstalledSequence: 1},
		{Status: "rolled_back", Code: "rolled_back", InstalledDigest: digest, InstalledVersion: "26.3.27", InstalledSequence: 1},
		{Status: "rejected", Code: "checksum_mismatch"},
		{Status: "failed_closed", Code: "history_invalid"},
	} {
		result.ID, result.NodeID, result.Revision, result.TargetDigest = "upd_cleared", nodeID, revision, digest
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		exec("UPDATE core_update_tasks SET result_json=?", data)
		if err := validateCoreUpdates(ctx, database); err != nil {
			t.Fatalf("cleared task with a stored %s result rejected: %v", result.Status, err)
		}
	}
	// Every other task status still has to carry exactly its own result status.
	exec("UPDATE core_update_tasks SET status='queued'")
	if err := validateCoreUpdates(ctx, database); err == nil {
		t.Fatal("a queued task with a stored result was accepted")
	}
}

package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"testing"
)

func hasRestoreIssue(result RestorePreview, code string) bool {
	for _, issue := range result.Issues {
		if issue == code {
			return true
		}
	}
	return false
}

func TestRestorePreviewReadsValidatedSnapshotAndCurrentUsageWithoutWritingBusinessState(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	for _, query := range []string{
		"INSERT INTO admins(id,username,role,status,password_hash,created_at) VALUES('owner','owner','Owner','active','test-hash','fixture')",
		"INSERT INTO users(id,username,display_name,status,expires_at,traffic_limit,used_traffic,max_devices,max_connections,protocols_json,remark,created_at) VALUES('user','user','User','active','',10000,100,2,8,'[\"http\"]','','fixture')",
		"INSERT INTO nodes(id,name,status,enabled,last_sequence,created_at) VALUES('node','Node','offline',1,5,'fixture')",
	} {
		if _, err := s.Repository.Database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	item, err := s.Create(ctx, "owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repository.Database.Exec("UPDATE users SET used_traffic=200,status='disabled' WHERE id='user'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repository.Database.Exec("UPDATE nodes SET last_sequence=10 WHERE id='node'"); err != nil {
		t.Fatal(err)
	}
	var writesBefore, writesAfter int64
	s.Repository.Database.QueryRow("SELECT total_changes()").Scan(&writesBefore)
	result, err := s.RestorePreview(ctx, item.ID, backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	s.Repository.Database.QueryRow("SELECT total_changes()").Scan(&writesAfter)
	if writesBefore != writesAfter {
		t.Fatal("restore preview altered live database")
	}
	if result.Snapshot.Users != 1 || result.Snapshot.Nodes != 1 || result.Snapshot.ActiveOwners != 1 || result.ExecutionAvailable || !result.SameKeyMaterial || !result.SameNodeCA {
		t.Fatal("wrong validated snapshot inventory", result)
	}
	if result.Impact.UsageRegressions != 1 || result.Impact.SequenceRegressions != 1 || result.Impact.UserReactivations != 1 {
		t.Fatal("rollback risk omitted", result.Impact)
	}
	if result.Snapshot.BackupFileOperations != 1 || !hasRestoreIssue(result, "backup_file_journal_reconciliation") {
		t.Fatal("snapshot own creation preparation was not flagged for restoration", result.Snapshot)
	}
	for _, code := range []string{"restore_executor_pending", "node_reconciliation_required", "message_cursor_regression", "observed_usage_regression", "user_reactivation"} {
		if !hasRestoreIssue(result, code) {
			t.Fatal("restore risk missing", code)
		}
	}
	encoded, _ := json.Marshal(result)
	for _, marker := range []string{"configuration.key", "NodeCADigest", "KeyDigest", "password_hash", backupTestPassword, "test-hash"} {
		if bytes.Contains(encoded, []byte(marker)) {
			t.Fatal("preview leaked confidential record", marker)
		}
	}
	entries, err := os.ReadDir(s.Directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("validated callback left plaintext materials")
		}
	}
	if _, err := s.RestorePreview(ctx, item.ID, "incorrect-restore-password"); !errors.Is(err, archive.ErrArchive) {
		t.Fatal("incorrect password previewed", err)
	}
	if _, err := s.RestorePreview(ctx, "missing", backupTestPassword); err == nil {
		t.Fatal("missing archive previewed")
	}
}

func TestForeignRestorePreviewFlagsMaterialChangesAndNeverClaimsExecutable(t *testing.T) {
	ctx := context.Background()
	source := backupFixture(t)
	target := backupFixture(t)
	item, err := source.Create(ctx, "source_owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(source.Directory, item.ID+".pcb"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := target.Import(ctx, "owner", "ip", backupTestPassword, bytes.NewReader(data), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	result, err := target.RestorePreview(ctx, item.ID, backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if result.SameKeyMaterial || result.SameNodeCA || result.ExecutionAvailable || !hasRestoreIssue(result, "configuration_key_change") || !hasRestoreIssue(result, "node_ca_change") || !hasRestoreIssue(result, "active_owner_required") {
		t.Fatal("foreign/ownerless archive deemed ready", result)
	}
}

func TestRestoreReconciliationSignalsIncludeRevocationsAndPendingOperations(t *testing.T) {
	snapshot := archive.Inventory{Totals: archive.Totals{Nodes: 2, Sessions: 2, PendingOperations: 1}, Nodes: map[string]archive.NodeRecord{"same": {ID: "same", Sequence: 1, RuntimeRevision: 1}, "new": {ID: "new"}}, Identities: map[string]archive.IdentityRecord{"same": {NodeID: "same", Fingerprint: "old"}}, Users: map[string]archive.UserRecord{"user": {ID: "user", Used: 100, Status: "active"}}}
	current := archive.Inventory{Totals: archive.Totals{Nodes: 2, PendingOperations: 1}, Nodes: map[string]archive.NodeRecord{"same": {ID: "same", Sequence: 10, RuntimeRevision: 2}, "removed": {ID: "removed"}}, Identities: map[string]archive.IdentityRecord{"same": {NodeID: "same", Fingerprint: "new", Revoked: true}}, Users: map[string]archive.UserRecord{"user": {ID: "user", Used: 200, Status: "disabled"}, "gone": {ID: "gone", Used: 20}}}
	result := compareRestore(snapshot, current)
	if result.Impact.NodesAdded != 1 || result.Impact.NodesRemoved != 1 || result.Impact.RuntimeDifferences != 1 || result.Impact.RevocationRegressions != 1 || result.Impact.UsersRemoved != 1 || result.Impact.UsageRegressions != 2 {
		t.Fatal(result.Impact)
	}
	for _, code := range []string{"session_revocation_required", "snapshot_pending_deployments", "current_pending_deployments", "certificate_revocation_regression"} {
		if !hasRestoreIssue(result, code) {
			t.Fatal("synchronization hazard not listed", code)
		}
	}
}

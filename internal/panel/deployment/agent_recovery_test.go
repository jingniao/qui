package deployment

import (
	"context"
	"encoding/json"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
	"testing"
)

func TestUnconfirmedAgentRecoveryKeepsLateOriginalResultSeparate(t *testing.T) {
	r, catalogue, nodes, nodeID, revision, entry, keys := agentTaskFixture(t)
	ctx := context.Background()
	if err := nodes.AgentUpdateSupport(ctx, nodeID, true, true); err != nil {
		t.Fatal(err)
	}
	task, err := r.QueueAgentUpdate(ctx, catalogue, keys, nodeID, entry.ID, "owner", "local", revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	confirm := AgentRecoveryConfirmation{NodeID: nodeID, Revision: revision.ID, CommandID: task.CommandID, Generation: 1, Unconfirmed: true}
	if _, err := r.RetryAgentRecovery(ctx, catalogue, task.ID, "owner", "local", confirm); err == nil {
		t.Fatal("queued command treated as unconfirmed execution")
	}
	if _, err := r.PendingAgentUpdate(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	confirm.Unconfirmed = false
	if _, err := r.RetryAgentRecovery(ctx, catalogue, task.ID, "owner", "local", confirm); err == nil {
		t.Fatal("implicit unconfirmed recovery accepted")
	}
	confirm.Unconfirmed = true
	next, err := r.RetryAgentRecovery(ctx, catalogue, task.ID, "owner", "local", confirm)
	if err != nil {
		t.Fatal(err)
	}
	result := release.UpdateResult{ID: task.CommandID, NodeID: nodeID, Revision: revision.ID, TargetDigest: entry.Signed.Manifest.SHA256, Status: "success", Code: "success", InstalledDigest: entry.Signed.Manifest.SHA256, InstalledVersion: entry.Signed.Manifest.Version, InstalledSequence: entry.Signed.Manifest.Sequence}
	data, _ := json.Marshal(result)
	if err := r.AgentUpdateResult(ctx, nodeID, &pb.AgentUpdateResult{ResultJson: data}); err != nil {
		t.Fatal(err)
	}
	items, _, err := r.ListAgentUpdates(ctx, 1, 20)
	if err != nil || len(items) != 1 || items[0].Status != "queued" || items[0].CommandID != next.CommandID || items[0].Result != nil {
		t.Fatal("late result ended recovery", items, err)
	}
	result.ID = next.CommandID
	data, _ = json.Marshal(result)
	if err := r.AgentUpdateResult(ctx, nodeID, &pb.AgentUpdateResult{ResultJson: data}); err != nil {
		t.Fatal(err)
	}
	attempts, total, err := r.ListAgentUpdateAttempts(ctx, task.ID, 1, 20)
	if err != nil || total != 2 || len(attempts) != 2 || attempts[0].Status != "success" || attempts[1].Status != "success" || attempts[1].Current {
		t.Fatal(attempts, total, err)
	}
}

func TestAgentRecoveryAttemptsKeepOriginalFailureAndRequireFreshCapability(t *testing.T) {
	r, catalogue, nodes, nodeID, revision, entry, keys := agentTaskFixture(t)
	ctx := context.Background()
	task, err := r.QueueAgentUpdate(ctx, catalogue, keys, nodeID, entry.ID, "owner", "local", revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err := r.PendingAgentUpdate(ctx, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := release.DecodeAgentUpdateRequest(command.RequestJson)
	if err != nil {
		t.Fatal(err)
	}
	failure := release.UpdateResult{ID: original.ID, NodeID: nodeID, Revision: revision.ID, TargetDigest: entry.Signed.Manifest.SHA256, Status: "failed_closed", Code: "installation_failed"}
	data, _ := json.Marshal(failure)
	if err := r.AgentUpdateResult(ctx, nodeID, &pb.AgentUpdateResult{ResultJson: data}); err != nil {
		t.Fatal(err)
	}
	confirm := AgentRecoveryConfirmation{NodeID: nodeID, Revision: revision.ID, CommandID: task.CommandID, Generation: 1}
	if _, err := r.RetryAgentRecovery(ctx, catalogue, task.ID, "owner", "local", confirm); err == nil {
		t.Fatal("legacy update-only node received recovery")
	}
	if err := nodes.AgentUpdateSupport(ctx, nodeID, true, true); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"node", "revision", "command", "generation"} {
		changed := confirm
		switch field {
		case "node":
			changed.NodeID = "other"
		case "revision":
			changed.Revision++
		case "command":
			changed.CommandID = "other"
		case "generation":
			changed.Generation++
		}
		if _, err := r.RetryAgentRecovery(ctx, catalogue, task.ID, "owner", "local", changed); err == nil {
			t.Fatal("stale recovery confirmation accepted", field)
		}
	}
	if _, err := r.RetryAgentRecovery(ctx, failingCoreAudit{catalogue}, task.ID, "owner", "local", confirm); err == nil {
		t.Fatal("audit failure committed recovery")
	}
	var attempts int
	if err := r.Database.QueryRow("SELECT count(*) FROM agent_update_attempts WHERE task_id=?", task.ID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatal("partial attempt", attempts, err)
	}
	next, err := r.RetryAgentRecovery(ctx, catalogue, task.ID, "owner", "local", confirm)
	if err != nil || next.Generation != 2 || next.OriginalID != original.ID || next.CommandID == original.ID {
		t.Fatal(next, err)
	}
	if _, err := r.RetryAgentRecovery(ctx, catalogue, task.ID, "owner", "local", confirm); err == nil {
		t.Fatal("same recovery confirmation reused")
	}
	if err := nodes.AgentUpdateSupport(ctx, nodeID, true); err != nil {
		t.Fatal(err)
	}
	if command, err := r.PendingAgentUpdate(ctx, nodeID); err != nil || command != nil {
		t.Fatal("recovery dispatched after capability downgrade", err)
	}
	if reserved, err := r.AgentUpdateReserved(ctx, nodeID); err != nil || !reserved {
		t.Fatal("downgrade released node", err)
	}
	if err := nodes.AgentUpdateSupport(ctx, nodeID, true, true); err != nil {
		t.Fatal(err)
	}
	command, err = r.PendingAgentUpdate(ctx, nodeID)
	if err != nil || command == nil {
		t.Fatal(err)
	}
	recovery, err := release.DecodeAgentUpdateRequest(command.RequestJson)
	if err != nil || recovery.Operation != "recover" || recovery.OriginalID != original.ID || recovery.Release != original.Release {
		t.Fatal(recovery, err)
	}
	// A late duplicate of the original failure cannot overwrite the new attempt.
	if err := r.AgentUpdateResult(ctx, nodeID, &pb.AgentUpdateResult{ResultJson: data}); err != nil {
		t.Fatal(err)
	}
	result := failure
	result.ID = recovery.ID
	result.Code = "recovery_failed"
	currentData, _ := json.Marshal(result)
	if err := r.AgentUpdateResult(ctx, nodeID, &pb.AgentUpdateResult{ResultJson: currentData}); err != nil {
		t.Fatal(err)
	}
	confirm.CommandID = next.CommandID
	confirm.Generation = 2
	third, err := r.RetryAgentRecovery(ctx, catalogue, task.ID, "owner", "local", confirm)
	if err != nil || third.Generation != 3 {
		t.Fatal(third, err)
	}
	var preserved []byte
	if err := r.Database.QueryRow("SELECT result_json FROM agent_update_attempts WHERE command_id=?", original.ID).Scan(&preserved); err != nil || string(preserved) != string(data) {
		t.Fatal("original failure changed", err)
	}
	items, total, err := r.ListAgentUpdateAttempts(ctx, task.ID, 1, 2)
	if err != nil || total != 3 || len(items) != 2 || !items[0].Current || items[0].Generation != 3 || items[0].Status != "queued" || items[1].Current || items[1].Result == nil || items[1].Result.Code != "recovery_failed" {
		t.Fatal("invalid current/history page", items, total, err)
	}
	items, total, err = r.ListAgentUpdateAttempts(ctx, task.ID, 2, 2)
	if err != nil || total != 3 || len(items) != 1 || items[0].Current || items[0].Result == nil || *items[0].Result != failure {
		t.Fatal("original failure pagination lost", items, total, err)
	}
}

package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/release"
	"strings"
	"testing"
)

func setMultiCore(t *testing.T, r *Repository, nodeID string, secondary bool) {
	t.Helper()
	c := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "xray", Version: "26.3.27"}}}
	if secondary {
		c.Cores = append(c.Cores, &pb.CoreCapability{Name: "sing-box", Version: "1.14.3"})
	}
	raw, err := protojson.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec("UPDATE nodes SET capabilities_json=?,multi_core_supported=1 WHERE id=?", string(raw), nodeID); err != nil {
		t.Fatal(err)
	}
}

func TestCoreUpdatesUseIndependentComponentsAndExactResults(t *testing.T) {
	r, audit, n, rev := coreNodeFixture(t, "linux/amd64", "xray")
	setMultiCore(t, r, n, false)
	ctx := context.Background()
	x := CoreUpdateInput{Component: "xray", Version: "26.3.28", URL: "https://github.com/XTLS/Xray-core/releases/download/v26.3.28/Xray-linux-64.zip", SHA256: strings.Repeat("ab", 32)}
	b := CoreUpdateInput{Component: "sing-box", Version: "1.14.3", URL: "https://github.com/SagerNet/sing-box/releases/download/v1.14.3/sing-box-1.14.3-linux-amd64.tar.gz", SHA256: strings.Repeat("cd", 32)}
	tx, err := r.QueueCoreUpdate(ctx, audit, n, "owner", "", rev.ID, x)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := r.QueueCoreUpdate(ctx, audit, n, "owner", "", rev.ID, b)
	if err != nil {
		t.Fatal("absent secondary addition blocked", err)
	}
	if tx.Sequence != 1 || tb.Sequence != 1 {
		t.Fatal(tx.Sequence, tb.Sequence)
	}
	if _, err := r.QueueCoreUpdate(ctx, audit, n, "owner", "", rev.ID, b); err == nil {
		t.Fatal("duplicate active component accepted")
	}
	commands := map[string]bool{}
	for i := 0; i < 2; i++ {
		command, err := r.PendingCoreUpdate(ctx, n)
		if err != nil || command == nil {
			t.Fatal(command, err)
		}
		commands[command.Component] = true
	}
	if len(commands) != 2 {
		t.Fatal("one component starved", commands)
	}
	result := release.UpdateResult{ID: tb.CommandID, NodeID: n, Revision: rev.ID, TargetDigest: tb.SHA256, Status: "success", Code: "success", InstalledDigest: strings.Repeat("ef", 32), InstalledVersion: tb.Version, InstalledSequence: tb.Sequence}
	raw, _ := json.Marshal(result)
	if err := r.CoreUpdateResult(ctx, n, &pb.CoreUpdateResult{Component: "xray", ResultJson: raw}); err == nil {
		t.Fatal("wrong envelope component accepted")
	}
	for i := 0; i < 2; i++ {
		if err := r.CoreUpdateResult(ctx, n, &pb.CoreUpdateResult{Component: "sing-box", ResultJson: raw}); err != nil {
			t.Fatal(err)
		}
	}
	result.Code = "changed"
	raw, _ = json.Marshal(result)
	if err := r.CoreUpdateResult(ctx, n, &pb.CoreUpdateResult{Component: "sing-box", ResultJson: raw}); err == nil {
		t.Fatal("changed idempotent result accepted")
	}
	// Task success alone must never fabricate refreshed enabled capabilities.
	var caps []byte
	if err := r.Database.QueryRow("SELECT capabilities_json FROM nodes WHERE id=?", n).Scan(&caps); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(caps), `"name":"sing-box"`) {
		t.Fatal("addition enabled before refresh")
	}
}

func TestRemoveCoreTasksRefuseAssignmentsAndReplayAfterRevisionChanges(t *testing.T) {
	r, audit, n, rev := coreNodeFixture(t, "linux/amd64", "xray")
	setMultiCore(t, r, n, true)
	ctx := context.Background()
	state, err := r.State(ctx, rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	state.Inbounds = []desiredstate.Inbound{{ID: "disabled", Core: "sing-box", Protocol: "socks5", Listen: "0.0.0.0", Port: 18888, Enabled: false, DefaultOutbound: "direct"}}
	with, err := r.Create(ctx, state, "owner", "disabled inbound")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Deploy(ctx, with.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Result(ctx, n, &pb.ApplyResult{Revision: with.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	_, err = r.QueueRemoveCore(ctx, audit, n, "sing-box", "owner", "", with.ID)
	var conflict *CoreRemovalConflict
	if !errors.As(err, &conflict) || conflict.Code != "core_in_use" || len(conflict.InboundIDs) != 1 {
		t.Fatal(err)
	}
	state.Inbounds = nil
	empty, err := r.Create(ctx, state, "owner", "unassign")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Deploy(ctx, empty.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Result(ctx, n, &pb.ApplyResult{Revision: empty.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	task, err := r.QueueRemoveCore(ctx, audit, n, "sing-box", "owner", "", empty.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		cmd, err := r.PendingRemoveCore(ctx, n)
		if err != nil || cmd == nil || cmd.Id != task.ID || cmd.Revision != empty.ID {
			t.Fatal(cmd, err)
		}
	}
	result := &pb.RemoveCoreResult{Id: task.ID, Component: "sing-box", Revision: empty.ID, Success: true, Code: "success"}
	if err := r.RemoveCoreResult(ctx, n, result); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec("UPDATE node_runtime_state SET revision=? WHERE node_id=?", rev.ID, n); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveCoreResult(ctx, n, result); err != nil {
		t.Fatal("late replay refused", err)
	}
	result.Component = "xray"
	if r.RemoveCoreResult(ctx, n, result) == nil {
		t.Fatal("changed remove identity accepted")
	}
	setMultiCore(t, r, n, false)
	_, err = r.QueueRemoveCore(ctx, audit, n, "xray", "owner", "", rev.ID)
	if !errors.As(err, &conflict) || conflict.Code != "last_core" {
		t.Fatal(err)
	}
}

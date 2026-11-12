package deployment

import (
	"context"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Telemetry of a revision the node received but has not confirmed yet is
// attributed to it (v1.5.2): refusing it broke the stream before the Agent's
// apply result could arrive. A failed revision stays refused, and so does one
// never dispatched to the node.
func TestAppliedStateAcceptsPendingRevision(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	first := f.publish(t, []string{"socks"})
	if _, err := f.repository.AppliedState(ctx, f.nodeID, first.ID); err != nil {
		t.Fatal("confirmed revision refused", err)
	}
	state := desiredstate.State{SchemaVersion: 1, NodeID: f.nodeID, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	pending, err := f.repository.Create(ctx, state, "owner", "pending")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repository.AppliedState(ctx, f.nodeID, pending.ID); err == nil {
		t.Fatal("revision never dispatched to the node accepted")
	}
	if err := f.repository.Deploy(ctx, pending.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := f.repository.AppliedState(ctx, f.nodeID, pending.ID); err != nil || got.NodeID != f.nodeID {
		t.Fatal("dispatched revision awaiting its result refused", err)
	}
	if err := f.repository.Result(ctx, f.nodeID, &pb.ApplyResult{Revision: pending.ID, Success: false, RolledBack: true, Code: "health_failed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repository.AppliedState(ctx, f.nodeID, pending.ID); err == nil {
		t.Fatal("failed revision accepted")
	}
}

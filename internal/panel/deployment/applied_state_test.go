package deployment

import (
	"context"
	"reflect"
	"testing"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Telemetry of a revision the node received but has not confirmed yet is
// attributed to it (v1.5.2): refusing it broke the stream before the Agent's
// apply result could arrive. A revision never dispatched to the node stays
// refused, while telemetry collected before a rollback remains attributable.
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
	if got, err := f.repository.AppliedState(ctx, f.nodeID, pending.ID); err != nil || got.Revision != pending.ID || got.NodeID != f.nodeID {
		t.Fatalf("rolled back revision attribution: %+v %v", got, err)
	}
}

func TestAppliedStateAcceptsDispatchedRevisionRegardlessOfStatus(t *testing.T) {
	for _, tc := range []struct {
		name, status, code string
	}{
		{"success", "success", "success"},
		{"pending", "pending", ""},
		{"rollback", "rollback", "apply_failed"},
		{"failed", "failed", "validation_failed"},
		{"canvas_remove_canceled", "failed", "canvas_remove_canceled"},
		{"superseded", "failed", "superseded"},
		{"canceled", "failed", "canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRecoveryFixture(t)
			ctx := context.Background()
			revision := f.publish(t, []string{"socks"})
			want, err := f.repository.State(ctx, revision.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.database.Exec("UPDATE deployment_targets SET status=?,result_code=? WHERE node_id=? AND revision=?", tc.status, tc.code, f.nodeID, revision.ID); err != nil {
				t.Fatal(err)
			}
			got, err := f.repository.AppliedState(ctx, f.nodeID, revision.ID)
			if err != nil {
				t.Fatal("dispatched revision refused", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("telemetry attribution state: got %+v, want %+v", got, want)
			}
		})
	}
}

func TestAppliedStateRejectsOtherNodeRevision(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	revision := f.publish(t, []string{"socks"})
	other, _, _, err := f.nodes.CreateEnrollment(ctx, "Other", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repository.AppliedState(ctx, other.ID, revision.ID); err == nil {
		t.Fatal("another node's revision accepted")
	}
	// Even a mismatched target row cannot override the node in the stored state.
	if _, err := f.database.Exec("INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(?,?,'failed',?)", revision.ID, other.ID, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repository.AppliedState(ctx, other.ID, revision.ID); err == nil {
		t.Fatal("revision with mismatched state node accepted")
	}
}

func TestAppliedStateRejectsUnknownRevision(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	revision := f.publish(t, []string{"socks"})
	if _, err := f.repository.AppliedState(ctx, f.nodeID, revision.ID+100); err == nil {
		t.Fatal("unknown revision accepted")
	}
}

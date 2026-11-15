package deployment

import (
	"context"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
)

func TestCanceledRevisionTelemetryAndLateResult(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	revision := f.publish(t, []string{"socks"})
	if _, err := f.repository.Database.Exec("UPDATE deployment_targets SET status='failed',result_code='canvas_remove_canceled' WHERE revision=?", revision.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repository.AppliedState(ctx, f.nodeID, revision.ID); err != nil {
		t.Fatal("real telemetry refused", err)
	}
	err := f.repository.Result(ctx, f.nodeID, &pb.ApplyResult{Revision: revision.ID, Success: true})
	if _, ok := err.(interface{ IgnoredDeploymentReason() string }); !ok {
		t.Fatalf("late result must be classified: %v", err)
	}
	if err := f.repository.Result(ctx, f.nodeID, &pb.ApplyResult{Revision: revision.ID, Code: "validation_failed"}); err == nil {
		t.Fatal("already failed target must also be classified for acknowledgement without notification")
	} else if _, ok := err.(interface{ IgnoredDeploymentReason() string }); !ok {
		t.Fatal(err)
	}
	status, err := f.repository.TargetStatus(ctx, f.nodeID, revision.ID)
	if err != nil || status != "failed" {
		t.Fatal(status, err)
	}
	for _, tc := range []struct {
		node     string
		revision int64
	}{{"foreign", revision.ID}, {f.nodeID, revision.ID + 100}} {
		if _, err := f.repository.AppliedState(ctx, tc.node, tc.revision); err == nil {
			t.Fatal("unknown revision accepted", tc)
		}
		if err := f.repository.Result(ctx, tc.node, &pb.ApplyResult{Revision: tc.revision, Success: true}); err == nil {
			t.Fatal("unknown result accepted", tc)
		}
	}
}

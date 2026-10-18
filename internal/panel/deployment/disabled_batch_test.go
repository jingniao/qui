package deployment

import (
	"context"
	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
)

func TestBatchDisabledWaitingNodeTriggersRollback(t *testing.T) {
	ctx := context.Background()
	r, states, _ := batchFixture(t)
	batch, err := r.CreateBatch(ctx, "chain", "node disabled mid-publication", "owner", states)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.StartBatch(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: r.Database}
	if err := nodes.Update(ctx, states[1].NodeID, "disabled middle", false); err != nil {
		t.Fatal(err)
	}
	first := batch.Items[0]
	if err := r.Result(ctx, first.NodeID, &pb.ApplyResult{Revision: first.Revision, Success: true}); err != nil {
		t.Fatal(err)
	}
	batch, err = r.Batch(ctx, batch.ID)
	if err != nil || batch.Status != "rolling_back" || batch.Items[1].Status != "failed" {
		t.Fatal("disabled node was queued instead of compensating", batch, err)
	}
	if pending, err := r.Pending(ctx, states[1].NodeID); err != nil || pending != nil {
		t.Fatal("disabled waiting node received configuration")
	}
}

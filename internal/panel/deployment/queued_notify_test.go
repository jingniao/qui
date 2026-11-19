package deployment

import (
	"context"
	"testing"
)

func TestQueuedDeploymentNotifiesAfterCommit(t *testing.T) {
	for _, batch := range []bool{false, true} {
		r, states, _ := batchFixture(t)
		ctx := context.Background()
		calls := 0
		r.Queued = func(revision int64) {
			calls++
			var state string
			if err := r.Database.QueryRowContext(ctx, "SELECT status FROM deployment_targets WHERE revision=?", revision).Scan(&state); err != nil || state != "pending" {
				t.Errorf("notification before committed pending: %q %v", state, err)
			}
		}
		var start func() error
		if batch {
			b, err := r.CreateBatch(ctx, "chain", "notify", "owner", batchSnapshots(states))
			if err != nil {
				t.Fatal(err)
			}
			start = func() error { return r.StartBatch(ctx, b.ID) }
		} else {
			revision, err := r.Create(ctx, states[0], "owner", "notify")
			if err != nil {
				t.Fatal(err)
			}
			start = func() error { return r.Deploy(ctx, revision.ID) }
		}
		if err := start(); err != nil {
			t.Fatal(err)
		}
		if err := start(); err != nil {
			t.Fatal(err)
		}
		if calls != 1 {
			t.Fatalf("batch=%v notifications=%d", batch, calls)
		}
	}
}

package spool

import (
	"context"
	"errors"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/access"
)

func enqueueAccess(t *testing.T, queue *Spool, domain string, blocked bool) string {
	t.Helper()
	result, err := queue.Enqueue(context.Background(), []access.Rollup{row(domain, blocked)})
	if err != nil || len(result.BatchIDs) != 1 {
		t.Fatalf("enqueue %s: %+v %v", domain, result, err)
	}
	return result.BatchIDs[0]
}

func quarantineAccess(t *testing.T, queue *Spool, batchID string) {
	t.Helper()
	for attempt := 1; attempt <= TrafficRejectLimit; attempt++ {
		quarantined, err := queue.RejectAccess(context.Background(), batchID)
		if err != nil {
			t.Fatal(err)
		}
		if quarantined != (attempt == TrafficRejectLimit) {
			t.Fatalf("rejection %d quarantined=%v", attempt, quarantined)
		}
	}
}

// A rejected access head leaves the queue head after the third rejection, later
// batches flow, the batch stays on disk, a restart retries it at once and an
// acknowledgement deletes it.
func TestRejectedAccessHeadIsQuarantinedAndLaterBatchesFlow(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	queue, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	rejected := enqueueAccess(t, queue, "rejected.example", false)
	later := enqueueAccess(t, queue, "later.example", false)
	quarantineAccess(t, queue, rejected)
	head, retried, err := queue.PendingRetry(ctx)
	if err != nil || head == nil || head.ID != later || retried {
		t.Fatalf("later batch did not become the head: %+v %v %v", head, retried, err)
	}
	// A report inside the cool-down is not a rejection of a delivery.
	if quarantined, err := queue.RejectAccess(ctx, rejected); err != nil || quarantined {
		t.Fatalf("rejecting inside the cool-down was not a no-op: %v %v", quarantined, err)
	}
	if err := queue.Ack(ctx, later); err != nil {
		t.Fatal(err)
	}
	if pending, err := queue.Pending(ctx); err != nil || pending != nil {
		t.Fatalf("quarantined batch ignored its cool-down: %+v %v", pending, err)
	}
	if stats, err := queue.Stats(ctx); err != nil || stats.Batches != 1 {
		t.Fatalf("quarantined access batch was not retained: %+v %v", stats, err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	pending, err := reopened.Pending(ctx)
	if err != nil || pending == nil || pending.ID != rejected {
		t.Fatalf("reopening did not release the quarantine: %+v %v", pending, err)
	}
	if err := reopened.Ack(ctx, rejected); err != nil {
		t.Fatal(err)
	}
	if stats, err := reopened.Stats(ctx); err != nil || stats.Batches != 0 {
		t.Fatalf("acknowledged batch not deleted: %+v %v", stats, err)
	}
}

// After the cool-down the quarantined batch returns, but only behind every
// deliverable batch; rejecting the retry quarantines it again at once.
func TestQuarantinedAccessReturnsAfterCoolDownOnlyWhenNothingElsePending(t *testing.T) {
	ctx := context.Background()
	shortenQuarantineCooldown(t, time.Minute)
	queue, err := Open(ctx, t.TempDir(), "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	rejected := enqueueAccess(t, queue, "rejected.example", false)
	later := enqueueAccess(t, queue, "later.example", false)
	quarantineAccess(t, queue, rejected)
	expire := func() {
		if _, err := queue.database.Exec("UPDATE batches SET quarantined_at=? WHERE id=?", time.Now().Add(-2*time.Hour).Unix(), rejected); err != nil {
			t.Fatal(err)
		}
	}
	expire()
	if pending, err := queue.Pending(ctx); err != nil || pending == nil || pending.ID != later {
		t.Fatalf("expired quarantine overtook the deliverable head: %+v %v", pending, err)
	}
	if err := queue.Ack(ctx, later); err != nil {
		t.Fatal(err)
	}
	retry, quarantined, err := queue.PendingRetry(ctx)
	if err != nil || retry == nil || retry.ID != rejected || !quarantined {
		t.Fatalf("expired quarantine was not retried: %+v %v %v", retry, quarantined, err)
	}
	if held, err := queue.RejectAccess(ctx, rejected); err != nil || !held {
		t.Fatalf("rejection of the retry did not re-quarantine: %v %v", held, err)
	}
	if pending, err := queue.Pending(ctx); err != nil || pending != nil {
		t.Fatalf("re-quarantined batch ignored its fresh cool-down: %+v %v", pending, err)
	}
}

// Quarantined access batches are retained like quarantined traffic batches:
// capacity eviction skips them.
func TestQuarantinedAccessBatchIsNotEvicted(t *testing.T) {
	ctx := context.Background()
	queue, err := Open(ctx, t.TempDir(), "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	kept := enqueueAccess(t, queue, "kept.example", false)
	quarantineAccess(t, queue, kept)
	stats, err := queue.Stats(ctx)
	if err != nil || stats.Bytes == 0 {
		t.Fatalf("stats: %+v %v", stats, err)
	}
	queue.limit = stats.Bytes
	if _, err := queue.Enqueue(ctx, []access.Rollup{row("new.example", true)}); !errors.Is(err, ErrFull) {
		t.Fatalf("quarantined access batch was evicted: %v", err)
	}
	if stats, err := queue.Stats(ctx); err != nil || stats.Batches != 1 || stats.DroppedAllowedRollups != 0 {
		t.Fatalf("quarantined batch lost: %+v %v", stats, err)
	}
}

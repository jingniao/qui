package spool

import (
	"context"
	"errors"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/traffic"
)

// A batch the panel rejects deterministically is retried by every session and
// blocks everything behind it. After the third rejection it leaves the head: the
// quarantine is kept on disk and counted, and later batches flow again. Its
// cool-down keeps it out even when nothing else is pending, and reopening the
// store retries it promptly.
func TestRejectedTrafficHeadIsQuarantinedAndLaterBatchesFlow(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	queue, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.SaveTraffic(ctx, []traffic.Counter{sample("epoch-rejected", 100)}); err != nil {
		t.Fatal(err)
	}
	if err := queue.SaveTraffic(ctx, []traffic.Counter{sample("epoch-later", 200)}); err != nil {
		t.Fatal(err)
	}
	head, err := queue.PendingTraffic(ctx)
	if err != nil || head == nil {
		t.Fatalf("no head batch: %+v %v", head, err)
	}
	quarantineAfterRejections(t, queue, head.ID)
	later, err := queue.PendingTraffic(ctx)
	if err != nil || later == nil || later.ID == head.ID {
		t.Fatalf("later batch did not become the head: %+v %v", later, err)
	}
	stats, err := queue.Stats(ctx)
	if err != nil || stats.TrafficQuarantined != 1 || stats.Batches != 2 {
		t.Fatalf("quarantined batch not counted: %+v %v", stats, err)
	}
	// A second report inside the cool-down is not a rejection of a delivery.
	if quarantined, err := queue.RejectTraffic(ctx, head.ID); err != nil || quarantined {
		t.Fatalf("rejecting a batch inside its cool-down was not a no-op: %v %v", quarantined, err)
	}
	if err := queue.AckTraffic(ctx, later.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := queue.PendingTraffic(ctx); err != nil || pending != nil {
		t.Fatalf("quarantined batch ignored its cool-down: %+v %v", pending, err)
	}
	// Quarantined rows keep counting toward the byte budget and are never
	// sacrificed: capacity eviction only removes allowed access rollups.
	final, err := queue.Stats(ctx)
	if err != nil || final.TrafficQuarantined != 1 || final.Batches != 1 || final.Bytes == 0 {
		t.Fatalf("retained quarantine no longer counted: %+v %v", final, err)
	}
	queue.limit = final.Bytes
	if _, err := queue.Enqueue(ctx, []access.Rollup{row("kept.example", true)}); !errors.Is(err, ErrFull) {
		t.Fatalf("quarantined traffic was evicted for capacity: %v", err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	// A restart releases the quarantine, so the batch is retried instead of
	// staying out forever.
	reopened, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	pending, err := reopened.PendingTraffic(ctx)
	if err != nil || pending == nil || pending.ID != head.ID {
		t.Fatalf("reopening did not release the quarantine: %+v %v", pending, err)
	}
	released, err := reopened.Stats(ctx)
	if err != nil || released.TrafficQuarantined != 0 || released.Batches != 1 {
		t.Fatalf("released batch still counted as quarantined: %+v %v", released, err)
	}
	if rejections, quarantinedAt := rowQuarantine(t, reopened, head.ID); rejections != 0 || quarantinedAt != 0 {
		t.Fatalf("released batch kept its quarantine state: %d %d", rejections, quarantinedAt)
	}
}

// The quarantine is a cool-down: once it expires the batch is retried, but only
// after every batch that is not quarantined. A rejection of that retry holds it
// back again with a fresh cool-down, and an acceptance deletes it as usual.
func TestQuarantinedTrafficReturnsAfterCoolDownOnlyWhenNothingElsePending(t *testing.T) {
	ctx := context.Background()
	shortenQuarantineCooldown(t, time.Minute)
	queue, err := Open(ctx, t.TempDir(), "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	if err := queue.SaveTraffic(ctx, []traffic.Counter{sample("epoch-rejected", 100)}); err != nil {
		t.Fatal(err)
	}
	if err := queue.SaveTraffic(ctx, []traffic.Counter{sample("epoch-later", 200)}); err != nil {
		t.Fatal(err)
	}
	head, err := queue.PendingTraffic(ctx)
	if err != nil || head == nil {
		t.Fatalf("no head batch: %+v %v", head, err)
	}
	quarantineAfterRejections(t, queue, head.ID)
	expireQuarantine(t, queue, head.ID)
	// The expired quarantine never overtakes a batch that is still deliverable.
	if pending, err := queue.PendingTraffic(ctx); err != nil || pending == nil || pending.ID == head.ID {
		t.Fatalf("expired quarantine overtook the deliverable head: %+v %v", pending, err)
	}
	later, err := queue.PendingTraffic(ctx)
	if err != nil || later == nil {
		t.Fatalf("no deliverable head: %+v %v", later, err)
	}
	if err := queue.AckTraffic(ctx, later.ID); err != nil {
		t.Fatal(err)
	}
	retried, quarantined, err := queue.PendingTrafficRetry(ctx)
	if err != nil || retried == nil || retried.ID != head.ID || !quarantined {
		t.Fatalf("expired quarantine was not retried as the last resort: %+v %v %v", retried, quarantined, err)
	}
	// A rejection of the retry keeps it quarantined with a fresh cool-down and
	// resets the counter so one more rejection quarantines it again.
	if held, err := queue.RejectTraffic(ctx, head.ID); err != nil || !held {
		t.Fatalf("rejection of the retried batch did not re-quarantine it: %v %v", held, err)
	}
	if pending, err := queue.PendingTraffic(ctx); err != nil || pending != nil {
		t.Fatalf("re-quarantined batch ignored its fresh cool-down: %+v %v", pending, err)
	}
	if rejections, quarantinedAt := rowQuarantine(t, queue, head.ID); rejections != TrafficRejectLimit-1 || quarantinedAt == 0 {
		t.Fatalf("re-quarantine did not reset the counter: %d %d", rejections, quarantinedAt)
	}
	if stats, err := queue.Stats(ctx); err != nil || stats.TrafficQuarantined != 1 || stats.Batches != 1 {
		t.Fatalf("re-quarantined batch not retained and counted: %+v %v", stats, err)
	}
	// A retry the panel accepts is deleted like any other delivery.
	expireQuarantine(t, queue, head.ID)
	if accepted, quarantined, err := queue.PendingTrafficRetry(ctx); err != nil || accepted == nil || accepted.ID != head.ID || !quarantined {
		t.Fatalf("expired quarantine was not retried again: %+v %v %v", accepted, quarantined, err)
	}
	if err := queue.AckTraffic(ctx, head.ID); err != nil {
		t.Fatal(err)
	}
	if stats, err := queue.Stats(ctx); err != nil || stats.Batches != 0 || stats.TrafficQuarantined != 0 {
		t.Fatalf("accepted retry was not deleted: %+v %v", stats, err)
	}
}

// An Agent restart or upgrade retries the held-back batches instead of letting a
// stale quarantine outlive the process that set it.
func TestReopeningSpoolReleasesTrafficQuarantine(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	queue, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.SaveTraffic(ctx, []traffic.Counter{sample("epoch-rejected", 100)}); err != nil {
		t.Fatal(err)
	}
	head, err := queue.PendingTraffic(ctx)
	if err != nil || head == nil {
		t.Fatalf("no head batch: %+v %v", head, err)
	}
	quarantineAfterRejections(t, queue, head.ID)
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	pending, err := reopened.PendingTraffic(ctx)
	if err != nil || pending == nil || pending.ID != head.ID {
		t.Fatalf("restart did not retry the quarantined batch: %+v %v", pending, err)
	}
	if stats, err := reopened.Stats(ctx); err != nil || stats.TrafficQuarantined != 0 {
		t.Fatalf("released batch still counted as quarantined: %+v %v", stats, err)
	}
	// The retried batch counts its rejections from zero again.
	if quarantined, err := reopened.RejectTraffic(ctx, head.ID); err != nil || quarantined {
		t.Fatalf("first rejection after the restart quarantined the batch: %v %v", quarantined, err)
	}
	if rejections, quarantinedAt := rowQuarantine(t, reopened, head.ID); rejections != 1 || quarantinedAt != 0 {
		t.Fatalf("released batch did not restart its rejection counter: %d %d", rejections, quarantinedAt)
	}
}

func quarantineAfterRejections(t *testing.T, queue *Spool, batchID string) {
	t.Helper()
	ctx := context.Background()
	for attempt := 1; attempt <= TrafficRejectLimit; attempt++ {
		quarantined, err := queue.RejectTraffic(ctx, batchID)
		if err != nil {
			t.Fatal(err)
		}
		if quarantined != (attempt == TrafficRejectLimit) {
			t.Fatalf("rejection %d quarantined=%v", attempt, quarantined)
		}
		if attempt < TrafficRejectLimit {
			pending, err := queue.PendingTraffic(ctx)
			if err != nil || pending == nil || pending.ID != batchID {
				t.Fatalf("head lost before the limit: %+v %v", pending, err)
			}
		}
	}
}

// expireQuarantine backdates the recorded quarantine so its cool-down is over
// without sleeping in the test.
func expireQuarantine(t *testing.T, queue *Spool, batchID string) {
	t.Helper()
	if _, err := queue.database.Exec("UPDATE traffic_batches SET quarantined_at=? WHERE id=?", time.Now().Add(-2*time.Hour).Unix(), batchID); err != nil {
		t.Fatal(err)
	}
}

func rowQuarantine(t *testing.T, queue *Spool, batchID string) (int64, int64) {
	t.Helper()
	var rejections, quarantinedAt int64
	if err := queue.database.QueryRow("SELECT rejections,quarantined_at FROM traffic_batches WHERE id=?", batchID).Scan(&rejections, &quarantinedAt); err != nil {
		t.Fatal(err)
	}
	return rejections, quarantinedAt
}

func shortenQuarantineCooldown(t *testing.T, cooldown time.Duration) {
	t.Helper()
	previous := TrafficQuarantineCooldown
	TrafficQuarantineCooldown = cooldown
	t.Cleanup(func() { TrafficQuarantineCooldown = previous })
}

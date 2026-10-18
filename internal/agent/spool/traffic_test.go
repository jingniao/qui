package spool

import (
	"context"
	"errors"
	"proxycontrol/pkg/contracts/traffic"
	"testing"
	"time"
)

func sample(epoch string, upload int64) traffic.Counter {
	return traffic.Counter{CoreEpoch: epoch, Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "socks5", UploadBytes: upload, DownloadBytes: 1000, ObservedAt: time.Now().UTC()}
}
func TestTrafficQueueCursorPersistenceNoResetAndAtomicFull(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	queue, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	counter := sample("epoch", 100)
	if err := queue.SaveTraffic(ctx, []traffic.Counter{counter}); err != nil {
		t.Fatal(err)
	}
	if err := queue.SaveTraffic(ctx, []traffic.Counter{counter}); err != nil {
		t.Fatal(err)
	}
	stats, _ := queue.Stats(ctx)
	if stats.Batches != 1 {
		t.Fatal("unchanged cumulative sample duplicated", stats)
	}
	queue.Close()
	queue, err = Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	batch, err := queue.PendingTraffic(ctx)
	if err != nil || batch == nil || batch.Counters[0].UploadBytes != 100 {
		t.Fatal("traffic restart lost sample", err)
	}
	if err := queue.AckTraffic(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	queue.limit = 64 // Inject exhausted storage budget after opening a valid queue.
	counter.UploadBytes = 200
	counter.ObservedAt = time.Now().UTC()
	if err := queue.SaveTraffic(ctx, []traffic.Counter{counter}); !errors.Is(err, ErrFull) {
		t.Fatal("capacity failure not reported", err)
	}
	queue.limit = DefaultLimit
	if err := queue.SaveTraffic(ctx, []traffic.Counter{counter}); err != nil {
		t.Fatal(err)
	}
	batch, err = queue.PendingTraffic(ctx)
	if err != nil || batch == nil || batch.Counters[0].UploadBytes != 200 {
		t.Fatal("failed write advanced cursor", err)
	}
	counter.UploadBytes = 150
	if err := queue.SaveTraffic(ctx, []traffic.Counter{counter}); err == nil {
		t.Fatal("same epoch decrease accepted")
	}
}

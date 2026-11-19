package node

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/testutil/dbtest"
)

func TestRuntimeCommitDoesNotWaitForReader(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	n, _, _, err := (&Repository{Database: reader}).CreateEnrollment(ctx, "node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	published := make(chan RuntimeSnapshot, 1)
	snapshots, err := NewRuntimeSnapshots(reader, func(event RuntimeSnapshot) { published <- event })
	if err != nil {
		t.Fatal(err)
	}
	reader.SetMaxOpenConns(1)
	conn, err := reader.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	enqueued := make(chan struct{})
	go func() {
		snapshots.afterCommit(n.ID)
		snapshots.afterCommit(n.ID)
		close(enqueued)
	}()
	select {
	case <-enqueued:
	case <-time.After(time.Second):
		t.Fatal("post-commit notification blocked waiting for a display connection")
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		snapshots.Run(ctx, slog.Default())
	}()
	select {
	case got := <-published:
		if got.Node.NodeID != n.ID || got.Node.Status != "pending" {
			t.Fatalf("published runtime = %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("worker did not publish the committed node")
	}
	cancel()
	<-workerDone
}

func TestRuntimeAdviceInputsInvalidateInventory(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repo := &Repository{Database: database}
	n, _, _, err := repo.CreateEnrollment(ctx, "server", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	const setBandwidth = `INSERT INTO node_meta(node_id,bandwidth_mbps) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET bandwidth_mbps=excluded.bandwidth_mbps`
	if _, err := database.ExecContext(ctx, setBandwidth, n.ID, 100); err != nil {
		t.Fatal(err)
	}
	published := make(chan RuntimeSnapshot, 6)
	snapshots, err := NewRuntimeSnapshots(database, func(event RuntimeSnapshot) { published <- event })
	if err != nil {
		t.Fatal(err)
	}
	initial, err := snapshots.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name, query string
		args        []any
	}{
		{"heartbeat tuning values", `UPDATE nodes SET heartbeat_json=? WHERE id=?`, []any{`{"tuningCurrent":{"net.ipv4.tcp_congestion_control":"bbr"}}`, n.ID}},
		{"bandwidth", setBandwidth, []any{n.ID, 200}},
	} {
		if _, err := database.ExecContext(ctx, change.query, change.args...); err != nil {
			t.Fatal(err)
		}
		// Polling restores the inventory revision even if no event was received.
		latest, err := snapshots.List(ctx)
		if err != nil || latest.InventoryRevision <= initial.InventoryRevision {
			t.Fatalf("%s failed to invalidate inventory: %#v, %v", change.name, latest, err)
		}
		initial = latest
		<-published
	}
	workerCtx, stopWorker := context.WithTimeout(ctx, 5*time.Second)
	defer stopWorker()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		snapshots.Run(workerCtx, slog.Default())
	}()
	defer func() { stopWorker(); <-workerDone }()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/any/tuning/recommendations/dismiss",
		strings.NewReader(`{"key":"net.core.default_qdisc","value":"fq"}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", n.ID)
	response := httptest.NewRecorder()
	(&Handler{Repository: repo, Runtime: snapshots}).dismissTuningRecommendation(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("dismissal returned %d: %s", response.Code, response.Body)
	}
	select {
	case got := <-published:
		if got.InventoryRevision <= initial.InventoryRevision {
			t.Fatalf("dismissal did not invalidate inventory: %+v", got)
		}
	case <-workerCtx.Done():
		t.Fatal("dismissal did not publish an inventory change")
	}
}

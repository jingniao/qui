package node_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/testutil/dbtest"
	"testing"
	"time"
)

func TestLivenessPublishesUsingReadPoolWhileWriterIsOccupied(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "runtime.db")
	database, err := dbtest.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repo := &node.Repository{Database: database}
	n, _, _, err := repo.CreateEnrollment(ctx, "node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec("UPDATE nodes SET status='degraded',last_heartbeat=? WHERE id=?", heartbeat, n.ID); err != nil {
		t.Fatal(err)
	}
	reader, err := db.OpenReadPool(ctx, path, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	events := make(chan node.RuntimeSnapshot, 16)
	store, err := node.NewRuntimeSnapshots(reader, func(e node.RuntimeSnapshot) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	live := node.NewLiveness(store.Changed)
	store.Liveness = live
	initial, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	held, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	done := make(chan struct{})
	go func() { defer close(done); store.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	defer func() { cancel(); <-done }()
	session := live.Connect(n.ID, "identity", time.Now())
	live.Establish(session)
	receipt := live.Receive(session, time.Now())
	await := func(check func(node.RuntimeNode) bool) node.RuntimeNode {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case e := <-events:
				if check(e.Node) {
					return e.Node
				}
			case <-timer.C:
				t.Fatal("volatile liveness waited for a write")
				return node.RuntimeNode{}
			}
		}
	}
	current := await(func(n node.RuntimeNode) bool { return n.Liveness.Connected && n.Liveness.InFlightCount == 1 })
	if current.LastHeartbeat != heartbeat || current.Status != "offline" || current.PersistedStatus != "degraded" || current.Revision <= initial.Revision {
		t.Fatalf("durable fields or revision: %+v", current)
	}
	live.SendingAck(session, receipt)
	live.Complete(session, receipt)
	live.Disconnect(session, time.Now())
	disconnected := await(func(n node.RuntimeNode) bool { return !n.Liveness.Connected && n.Liveness.InFlightCount == 0 })
	if disconnected.Revision <= current.Revision {
		t.Fatal("disconnect did not advance revision")
	}
}

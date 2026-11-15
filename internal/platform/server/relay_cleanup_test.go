package server

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

func TestRelayCleanupControlQueuesOnCommandPoll(t *testing.T) {
	f := newQuotaFixture(t)
	f.publish(t)
	ctx := context.Background()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := f.app.Database.ExecContext(ctx, "UPDATE nodes SET status='offline',last_heartbeat='' WHERE id=?", f.nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.Database.ExecContext(ctx, "DELETE FROM inbounds WHERE node_id=?", f.nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.Database.ExecContext(ctx, "INSERT INTO relay_cleanup_nodes(node_id,reason,next_attempt_at,created_at,updated_at) VALUES(?,'removed',?,?,?)", f.nodeID, stamp, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	control := &relayCleanupControl{Repository: f.app.deployments, cleanup: f.app.relayCleanup}
	command, err := control.Pending(ctx, f.nodeID)
	if err != nil || command == nil {
		t.Fatalf("cleanup not queued on control poll: %v", err)
	}
	state, err := desiredstate.Decode(command.DesiredStateJson)
	if err != nil || len(state.Inbounds) != 0 {
		t.Fatalf("deleted inbound remains: %+v %v", state.Inbounds, err)
	}
	var completed int
	if err := f.app.Database.QueryRowContext(ctx, "SELECT completed FROM relay_cleanup_reconcile WHERE id=1").Scan(&completed); err != nil || completed != 1 {
		t.Fatalf("startup cleanup marker %d: %v", completed, err)
	}
}

func TestRelayCleanupPreparedRestoreDefersWrites(t *testing.T) {
	app := testServer(t)
	ctx := context.Background()
	if _, err := app.Database.ExecContext(ctx, "UPDATE relay_cleanup_reconcile SET completed=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Database.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	defer app.Database.ExecContext(ctx, "PRAGMA query_only=OFF")
	prepared, err := newServer(app.Config, app.Database, fstest.MapFS{"index.html": {Data: []byte("panel")}}, app.Logger, true)
	if err != nil {
		t.Fatalf("read-only restore preparation: %v", err)
	}
	defer prepared.closeReadPool()
	var completed int
	if err := app.Database.QueryRowContext(ctx, "SELECT completed FROM relay_cleanup_reconcile WHERE id=1").Scan(&completed); err != nil || completed != 0 {
		t.Fatalf("preparation changed cleanup marker %d: %v", completed, err)
	}
	if _, err := app.Database.ExecContext(ctx, "PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	if err := prepared.tunnelPublications.ReconcileRelayCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.Database.QueryRowContext(ctx, "SELECT completed FROM relay_cleanup_reconcile WHERE id=1").Scan(&completed); err != nil || completed != 1 {
		t.Fatalf("handoff cleanup marker %d: %v", completed, err)
	}
}

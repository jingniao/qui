package server

import (
	"context"
	"testing"

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
}

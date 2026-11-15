package deployment

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strconv"
	"testing"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func relayCleanupFixture(t *testing.T) (*RelayCleanupDeployer, string, desiredstate.State) {
	t.Helper()
	r, nodes, nodeID, pending := closeTasksFixture(t)
	ctx := context.Background()
	exec(t, r, ctx, `UPDATE nodes SET capabilities_json='{"coreName":"xray","forwardInboundSupported":true}' WHERE id=?`, nodeID)
	if err := r.Result(ctx, nodeID, &pb.ApplyResult{Revision: pending.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	state := desiredstate.Baseline(nodeID, nil)
	state.Credentials = []desiredstate.Credential{{ID: "internal", Protocol: "socks5", Username: "relay", Secret: "secret", Internal: true}}
	state.Inbounds = []desiredstate.Inbound{{ID: "removed", Protocol: "socks5", Listen: "127.0.0.1", Port: 12345, Enabled: true, CredentialIDs: []string{"internal"}, DefaultOutbound: "direct"}, {ID: "keep", Protocol: "forward", Listen: "127.0.0.1", Port: 12346, Enabled: false, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp"}}}}
	state.Logging.DomainOnly = true
	revision, err := r.CreateWithInboundIntent(ctx, state, []string{"removed"}, "owner", "published")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Result(ctx, nodeID, &pb.ApplyResult{Revision: revision.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	exec(t, r, ctx, `INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('keep','unpublished edit',?,'forward','0.0.0.0',12347,1,0,0,'block','[]',x'00','x')`, nodeID)
	now := time.Now().UTC()
	exec(t, r, ctx, "UPDATE nodes SET status='online',last_heartbeat=? WHERE id=?", now.Format(time.RFC3339Nano), nodeID)
	exec(t, r, ctx, "INSERT INTO relay_cleanup_nodes(node_id,reason,next_attempt_at,created_at,updated_at) VALUES(?,'removed scheme',?,?,?)", nodeID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	d := NewRelayCleanupDeployer(r, nodes, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.Now = func() time.Time { return now }
	state.Revision = revision.ID
	return d, nodeID, state
}

func TestRelayCleanupOfflineThenOnlyDeletesPublishedObjects(t *testing.T) {
	d, nodeID, published := relayCleanupFixture(t)
	ctx := context.Background()
	exec(t, d.repository, ctx, "UPDATE nodes SET status='offline' WHERE id=?", nodeID)
	if err := d.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if got := text(t, d.repository, ctx, "SELECT coalesce(revision_id,0) FROM relay_cleanup_nodes WHERE node_id=?", nodeID); got != "0" {
		t.Fatal(got)
	}
	exec(t, d.repository, ctx, "UPDATE nodes SET status='online' WHERE id=?", nodeID)
	if err := d.ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	command, err := d.repository.Pending(ctx, nodeID)
	if err != nil || command == nil {
		t.Fatal("cleanup not queued", err)
	}
	next, err := desiredstate.Decode(command.DesiredStateJson)
	if err != nil {
		t.Fatal(err)
	}
	published.Revision = next.Revision
	published.Inbounds = published.Inbounds[1:]
	published.Credentials = []desiredstate.Credential{}
	if !reflect.DeepEqual(next, published) {
		t.Fatalf("changed published fields beyond deletions\nnext: %#v\nwant: %#v", next, published)
	}
	intent, err := d.repository.InboundIntent(ctx, next.Revision)
	if err != nil || len(intent) != 0 || intent == nil {
		t.Fatal("intent", intent, err)
	}
	if got := text(t, d.repository, ctx, "SELECT author||':'||summary FROM config_revisions WHERE id=?", next.Revision); got != "auto:auto: relay cleanup" {
		t.Fatal(got)
	}
}

func TestRelayCleanupConnectedCapabilityPollDoesNotWaitForHeartbeat(t *testing.T) {
	d, nodeID, _ := relayCleanupFixture(t)
	ctx := context.Background()
	exec(t, d.repository, ctx, "UPDATE nodes SET status='offline',last_heartbeat='' WHERE id=?", nodeID)
	if err := d.ReconcileConnectedNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if command, err := d.repository.Pending(ctx, nodeID); err != nil || command == nil {
		t.Fatalf("live control connection did not queue cleanup before first heartbeat: %v", err)
	}
}

func TestRelayCleanupDeletedNodeClearsPendingDuringBackoff(t *testing.T) {
	d, nodeID, _ := relayCleanupFixture(t)
	ctx := context.Background()
	exec(t, d.repository, ctx, "UPDATE nodes SET deleted_at=? WHERE id=?", d.Now().Format(time.RFC3339Nano), nodeID)
	exec(t, d.repository, ctx, "UPDATE relay_cleanup_nodes SET attempts=3,next_attempt_at=? WHERE node_id=?", d.Now().Add(30*time.Minute).Format(time.RFC3339Nano), nodeID)
	if err := d.ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if got := text(t, d.repository, ctx, "SELECT count(*) FROM relay_cleanup_nodes WHERE node_id=?", nodeID); got != "0" {
		t.Fatal("deleted node still pending", got)
	}
}

func TestRelayCleanupNewRemovalWaitsForPendingRevision(t *testing.T) {
	d, nodeID, _ := relayCleanupFixture(t)
	ctx := context.Background()
	if err := d.ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := d.repository.Database.QueryRowContext(ctx, "SELECT revision_id FROM relay_cleanup_nodes WHERE node_id=?", nodeID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	exec(t, d.repository, ctx, "DELETE FROM inbounds WHERE id='keep'")
	// Registration preserves an in-flight revision while making the new work due.
	exec(t, d.repository, ctx, "UPDATE relay_cleanup_nodes SET attempts=0,last_code='',next_attempt_at=?,updated_at=? WHERE node_id=?", d.Now().Format(time.RFC3339Nano), d.Now().Add(time.Nanosecond).Format(time.RFC3339Nano), nodeID)
	if err := d.ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if got := text(t, d.repository, ctx, "SELECT count(*) FROM config_revisions WHERE summary='auto: relay cleanup'"); got != "1" {
		t.Fatal("deployed conflicting cleanup revision", got)
	}
	if err := d.repository.Result(ctx, nodeID, &pb.ApplyResult{Revision: revision, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if got := text(t, d.repository, ctx, "SELECT count(*) FROM config_revisions WHERE summary='auto: relay cleanup'"); got != "2" {
		t.Fatal("new removal was lost after pending revision succeeded", got)
	}
	command, err := d.repository.Pending(ctx, nodeID)
	if err != nil || command == nil {
		t.Fatal("follow-up cleanup not pending", err)
	}
	state, err := desiredstate.Decode(command.DesiredStateJson)
	if err != nil || len(state.Inbounds) != 0 {
		t.Fatalf("follow-up retained deleted inbound: %+v %v", state.Inbounds, err)
	}
}

func TestRelayCleanupFailureBackoffAndRestartPending(t *testing.T) {
	d, nodeID, _ := relayCleanupFixture(t)
	ctx := context.Background()
	for index, delay := range []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		if err := d.ReconcileNode(ctx, nodeID); err != nil {
			t.Fatal(err)
		}
		command, err := d.repository.Pending(ctx, nodeID)
		if err != nil || command == nil {
			t.Fatal("pending", err)
		}
		state, err := desiredstate.Decode(command.DesiredStateJson)
		if err != nil {
			t.Fatal(err)
		}
		restarted := NewRelayCleanupDeployer(d.repository, d.nodes, d.logger)
		restarted.Now = d.Now
		if err := restarted.ReconcileNode(ctx, nodeID); err != nil {
			t.Fatal(err)
		}
		if got := text(t, d.repository, ctx, "SELECT count(*) FROM config_revisions WHERE summary='auto: relay cleanup'"); got != strconv.Itoa(index+1) {
			t.Fatal("duplicate after restart", got)
		}
		if err := d.repository.Result(ctx, nodeID, &pb.ApplyResult{Revision: state.Revision, RolledBack: true, Code: "health_failed"}); err != nil {
			t.Fatal(err)
		}
		before := d.Now()
		if err := d.ReconcileNode(ctx, nodeID); err != nil {
			t.Fatal(err)
		}
		if got := text(t, d.repository, ctx, "SELECT last_code FROM relay_cleanup_nodes WHERE node_id=?", nodeID); got != "health_failed" {
			t.Fatal(got)
		}
		at, err := time.Parse(time.RFC3339Nano, text(t, d.repository, ctx, "SELECT next_attempt_at FROM relay_cleanup_nodes WHERE node_id=?", nodeID))
		if err != nil || at.Sub(before) != delay {
			t.Fatal("backoff", at.Sub(before), err)
		}
		if err := d.ReconcileNode(ctx, nodeID); err != nil {
			t.Fatal(err)
		}
		if p, err := d.repository.Pending(ctx, nodeID); err != nil || p != nil {
			t.Fatal("retried before due", err)
		}
		d.Now = func() time.Time { return at }
	}
}

func TestRelayCleanupAlreadyRemovedClearsPending(t *testing.T) {
	d, nodeID, _ := relayCleanupFixture(t)
	ctx := context.Background()
	if err := d.ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	command, err := d.repository.Pending(ctx, nodeID)
	if err != nil || command == nil {
		t.Fatal(err)
	}
	state, err := desiredstate.Decode(command.DesiredStateJson)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.repository.Result(ctx, nodeID, &pb.ApplyResult{Revision: state.Revision, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if got := text(t, d.repository, ctx, "SELECT count(*) FROM relay_cleanup_nodes WHERE node_id=?", nodeID); got != "0" {
		t.Fatal(got)
	}
}

func TestRelayCleanupBusyDoesNotCountFailure(t *testing.T) {
	d, nodeID, published := relayCleanupFixture(t)
	ctx := context.Background()
	other, err := d.repository.Create(ctx, published, "owner", "owner deployment")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.repository.Deploy(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if got := text(t, d.repository, ctx, "SELECT attempts||':'||last_code||':'||coalesce(revision_id,0) FROM relay_cleanup_nodes WHERE node_id=?", nodeID); got != "0::0" {
		t.Fatal(got)
	}
	if got := text(t, d.repository, ctx, "SELECT count(*) FROM config_revisions WHERE summary='auto: relay cleanup'"); got != "0" {
		t.Fatal("created revision while busy", got)
	}
}

func TestRelayCleanupRebuildsOvertakenDraft(t *testing.T) {
	d, nodeID, published := relayCleanupFixture(t)
	ctx := context.Background()
	old, err := d.repository.Create(ctx, published, "auto", "auto: relay cleanup")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, d.repository, ctx, "UPDATE relay_cleanup_nodes SET revision_id=? WHERE node_id=?", old.ID, nodeID)
	published.Inbounds[1].Port = 13001
	owner, err := d.repository.CreateWithInboundIntent(ctx, published, []string{"keep"}, "owner", "owner changed port")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.repository.Deploy(ctx, owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.repository.Result(ctx, nodeID, &pb.ApplyResult{Revision: owner.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	command, err := d.repository.Pending(ctx, nodeID)
	if err != nil || command == nil {
		t.Fatal(err)
	}
	next, err := desiredstate.Decode(command.DesiredStateJson)
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision == old.ID || next.Inbounds[0].Port != 13001 {
		t.Fatal("deployed stale draft", next.Revision, next.Inbounds)
	}
	intent, err := d.repository.InboundIntent(ctx, next.Revision)
	if err != nil || !reflect.DeepEqual(intent, []string{"keep"}) {
		t.Fatal("lost current published intent", intent, err)
	}
}

func TestRelayCleanupPreservesLiveChainContinuation(t *testing.T) {
	d, _, published := relayCleanupFixture(t)
	ctx := context.Background()
	exec(t, d.repository, ctx, `INSERT INTO proxy_chains(id,name,node_ids_json,final_outbound_id,support_tcp,support_udp,enabled,failure_policy,created_at) VALUES('live-chain','live chain','[]','direct',1,0,1,'Block','x')`)
	published.Outbounds = append(published.Outbounds, desiredstate.Outbound{ID: "_chain_continue_live-chain", Protocol: "socks5", Address: "example.com", Port: 1080, Enabled: true, CredentialID: "internal"})
	next, _, err := d.withoutDeleted(ctx, published, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Outbounds) != 3 || next.Outbounds[2].ID != "_chain_continue_live-chain" || len(next.Credentials) != 1 {
		t.Fatal("removed surviving chain material", next.Outbounds, next.Credentials)
	}
	if len(published.Inbounds) != 2 || len(published.Credentials) != 1 {
		t.Fatal("mutated published state")
	}
}

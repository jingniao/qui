package deployment

import (
	"context"
	"errors"
	"testing"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
)

// relaySourceAndTarget builds one relay outbound on the first fixture node whose
// target is the second, and returns the captured snapshots in an order that puts
// the target last - what a drawing does when it orders a GOST tunnel's exit
// first. The target's inbound carries the credential the source dials.
func relaySourceAndTarget(states []desiredstate.State) []proxy.NodeSnapshot {
	credential := desiredstate.Credential{ID: "relay", Protocol: "socks5", Username: "relay", Secret: "relay-test-secret", Internal: true}
	states[1].Credentials = []desiredstate.Credential{credential}
	states[1].Inbounds[0].CredentialIDs = []string{credential.ID}
	states[0].Credentials = []desiredstate.Credential{credential}
	states[0].Outbounds = append(states[0].Outbounds, desiredstate.Outbound{ID: "next", Protocol: "socks5", Address: "127.0.0.1", Port: states[1].Inbounds[0].Port, TargetNodeID: states[1].NodeID, CredentialID: credential.ID, Enabled: true})
	return []proxy.NodeSnapshot{{State: states[0]}, {State: states[1]}}
}

// A batch may list a relay target after its source only when that target already
// runs the exact state the source dials. The published revision is what decides,
// and the queue re-checks it at dispatch (relayTargetsInTransaction), so an
// unpublished or mismatched target is still refused before any revision exists.
func TestBatchValidationAcceptsOnlyAnAlreadyPublishedLaterTarget(t *testing.T) {
	ctx := context.Background()
	r, states, _ := batchFixture(t)
	captured := relaySourceAndTarget(states)
	if _, err := r.CreateBatch(ctx, "", "later target", "owner", captured); !errors.Is(err, ErrDependency) {
		t.Fatalf("a later target that published nothing was accepted: %v", err)
	}
	// The target published something else: the source would dial a listener that
	// is not there.
	stale := states[1]
	stale.Inbounds = []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Enabled: true, Listen: "127.0.0.1", Port: 2699, DefaultOutbound: "direct"}}
	applySnapshot(t, r, stale)
	if _, err := r.CreateBatch(ctx, "", "later target", "owner", captured); !errors.Is(err, ErrDependency) {
		t.Fatalf("a later target with another published state was accepted: %v", err)
	}
	// Same protocol, port and credential but other listener settings (e.g. a new
	// transport) is not the running listener either.
	changed := states[1]
	changed.Inbounds = append([]desiredstate.Inbound(nil), states[1].Inbounds...)
	changed.Inbounds[0].Settings.Transport = "ws"
	applySnapshot(t, r, changed)
	if _, err := r.CreateBatch(ctx, "", "later target", "owner", captured); !errors.Is(err, ErrDependency) {
		t.Fatalf("a later target running other listener settings was accepted: %v", err)
	}
	// Once the target runs the captured state, the drawing's own order may put it
	// last: the publication is allowed and the queue still re-checks the target at
	// dispatch.
	applySnapshot(t, r, states[1])
	if _, err := r.CreateBatch(ctx, "", "later target", "owner", captured); err != nil {
		t.Fatalf("a later target that already runs its captured state was refused: %v", err)
	}
	// A target outside the batch and a self-target stay refused.
	if _, err := r.CreateBatch(ctx, "", "missing target", "owner", []proxy.NodeSnapshot{{State: states[0]}, {State: states[2]}}); !errors.Is(err, ErrDependency) {
		t.Fatalf("a target outside the batch was accepted: %v", err)
	}
	self := states[0]
	self.Outbounds = append(self.Outbounds, desiredstate.Outbound{ID: "self", Protocol: "socks5", Address: "127.0.0.1", Port: self.Inbounds[0].Port, TargetNodeID: self.NodeID, CredentialID: "relay", Enabled: true})
	if _, err := r.CreateBatch(ctx, "", "self target", "owner", []proxy.NodeSnapshot{{State: self}, {State: states[1]}}); !errors.Is(err, ErrDependency) {
		t.Fatalf("a relay outbound targeting its own node was accepted: %v", err)
	}
}

// A draft batch is checked again when it starts: the later target's running
// listener may have changed after the batch was created.
func TestStartBatchRechecksALaterTargetsListener(t *testing.T) {
	ctx := context.Background()
	r, states, _ := batchFixture(t)
	captured := relaySourceAndTarget(states)
	applySnapshot(t, r, states[1])
	batch, err := r.CreateBatch(ctx, "", "later target", "owner", captured)
	if err != nil {
		t.Fatal(err)
	}
	changed := states[1]
	changed.Inbounds = append([]desiredstate.Inbound(nil), states[1].Inbounds...)
	changed.Inbounds[0].Settings.Transport = "ws"
	applySnapshot(t, r, changed)
	if err := r.StartBatch(ctx, batch.ID); !errors.Is(err, ErrDependency) {
		t.Fatalf("a batch started against a changed target listener: %v", err)
	}
}

package connections

import (
	"errors"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/traffic"
)

type stepClock struct{ now time.Time }

func (c *stepClock) advance(d time.Duration) { c.now = c.now.Add(d) }

func snapshot(nodeID, credentialID string, count uint64) traffic.ConnectionSnapshot {
	return traffic.ConnectionSnapshot{
		NodeID: nodeID, Revision: 1, CoreEpoch: "epoch", ObservedAt: time.Now().UTC(),
		Scope: traffic.ConnectionScope, Connections: count,
		Accounts: []traffic.ConnectionAccount{{CredentialID: credentialID, Connections: count}},
	}
}

func TestStoreKeepsOnlyFreshSnapshots(t *testing.T) {
	clock := &stepClock{now: time.Unix(1_000, 0)}
	store := NewStore(45 * time.Second)
	store.Now = func() time.Time { return clock.now }

	if _, ok := store.Snapshot("node-a"); ok {
		t.Fatal("empty store reported a snapshot")
	}
	if err := store.Record("node-a", snapshot("node-a", "cred-1", 3)); err != nil {
		t.Fatal(err)
	}
	if current, ok := store.Snapshot("node-a"); !ok || current.Connections != 3 {
		t.Fatalf("fresh snapshot missing: %+v %v", current, ok)
	}
	clock.advance(45 * time.Second)
	if _, ok := store.Snapshot("node-a"); ok {
		t.Fatal("expired snapshot still reported")
	}
	if got := store.Snapshots(); len(got) != 0 {
		t.Fatalf("expired snapshots returned: %+v", got)
	}
}

func TestStoreRejectsMismatchedAndInvalidSnapshots(t *testing.T) {
	clock := &stepClock{now: time.Unix(1_000, 0)}
	store := NewStore(45 * time.Second)
	store.Now = func() time.Time { return clock.now }

	if err := store.Record("node-a", snapshot("node-b", "cred-1", 1)); !errors.Is(err, ErrNodeMismatch) {
		t.Fatalf("node mismatch accepted: %v", err)
	}
	invalid := snapshot("node-a", "cred-1", 1)
	invalid.Connections = 99
	if err := store.Record("node-a", invalid); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	if _, ok := store.Snapshot("node-a"); ok {
		t.Fatal("rejected snapshot was stored")
	}
}

func TestSumRestrictsToGivenCredentials(t *testing.T) {
	snapshots := []traffic.ConnectionSnapshot{
		snapshot("node-a", "cred-1", 2),
		snapshot("node-b", "cred-2", 5),
	}
	if got := Sum(snapshots, map[string]bool{"cred-1": true}); got != 2 {
		t.Fatalf("sum = %d, want 2", got)
	}
	if got := Sum(snapshots, map[string]bool{"cred-1": true, "cred-2": true}); got != 7 {
		t.Fatalf("sum = %d, want 7", got)
	}
	if got := Sum(snapshots, map[string]bool{"cred-3": true}); got != 0 {
		t.Fatalf("sum = %d, want 0", got)
	}
}

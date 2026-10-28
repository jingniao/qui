package user

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/connections"
	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/traffic"
)

type fakeGroups map[string]node.Group

func (f fakeGroups) Group(_ context.Context, id string) (node.Group, error) {
	group, ok := f[id]
	if !ok {
		return node.Group{}, node.ErrNotFound
	}
	return group, nil
}

func observationSnapshot(nodeID, credentialID string, count uint64) traffic.ConnectionSnapshot {
	return traffic.ConnectionSnapshot{
		NodeID: nodeID, Revision: 1, CoreEpoch: "epoch", ObservedAt: time.Now().UTC(),
		Scope: traffic.ConnectionScope, Connections: count,
		Accounts: []traffic.ConnectionAccount{{CredentialID: credentialID, Connections: count}},
	}
}

// seedObservedUser creates a user with one credential on group g1/node n1, and
// returns the observer with a controllable clock.
func seedObservedUser(t *testing.T, maxConnections int) (*Repository, *ConnectionObserver, *time.Time, User, string) {
	t.Helper()
	ctx := context.Background()
	repository := testRepository(t)
	input := userInput()
	input.MaxConnections = maxConnections
	account, err := repository.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	device, err := repository.CreateDevice(ctx, account.ID, DeviceInput{Name: "Laptop", Protocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	credentialID := device.Credentials[0].ID
	if _, err := repository.Database.ExecContext(ctx, "INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,remark,created_at) VALUES('g1','g1','subscription',1,1,'',?)", timestamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := repository.Grant(ctx, account.ID, "g1", false); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2_000, 0)
	store := connections.NewStore(45 * time.Second)
	store.Now = func() time.Time { return now }
	observer := &ConnectionObserver{
		Repository: repository,
		Nodes:      fakeGroups{"g1": {ID: "g1", NodeIDs: []string{"n1"}}},
		Store:      store,
	}
	return repository, observer, &now, account, credentialID
}

func TestForUserReportsCountsAndUnknown(t *testing.T) {
	ctx := context.Background()
	_, observer, now, account, credentialID := seedObservedUser(t, 5)

	count, err := observer.ForUser(ctx, account)
	if err != nil || count != nil {
		t.Fatalf("count before any snapshot = %v (%v), want unknown nil", count, err)
	}
	if err := observer.Store.Record("n1", observationSnapshot("n1", credentialID, 3)); err != nil {
		t.Fatal(err)
	}
	count, err = observer.ForUser(ctx, account)
	if err != nil || count == nil || *count != 3 {
		t.Fatalf("count = %v (%v), want 3", count, err)
	}
	// A snapshot from an unrelated node in another group must not count.
	if err := observer.Store.Record("n2", observationSnapshot("n2", credentialID, 9)); err != nil {
		t.Fatal(err)
	}
	count, err = observer.ForUser(ctx, account)
	if err != nil || count == nil || *count != 3 {
		t.Fatalf("unrelated node leaked into count: %v (%v)", count, err)
	}
	// Ageing past the freshness window turns the count back into unknown.
	*now = now.Add(45 * time.Second)
	count, err = observer.ForUser(ctx, account)
	if err != nil || count != nil {
		t.Fatalf("stale snapshot = %v (%v), want unknown nil", count, err)
	}
}

// TestForUserNeedsAnEntryForTheUser pins the three states the count can be in:
// unknown while no snapshot names one of the user's credentials, an explicit
// zero entry, and the sum of the entries the reporting nodes sent.
func TestForUserNeedsAnEntryForTheUser(t *testing.T) {
	ctx := context.Background()
	_, observer, _, account, credentialID := seedObservedUser(t, 5)
	observer.Nodes = fakeGroups{"g1": {ID: "g1", NodeIDs: []string{"n1", "n2"}}}

	// A fresh snapshot naming only another credential is not a report about
	// this user: a WireGuard-only user gets no online entry at all, so the
	// count stays unknown instead of turning into an invented zero.
	if err := observer.Store.Record("n1", observationSnapshot("n1", "credential-other", 4)); err != nil {
		t.Fatal(err)
	}
	count, err := observer.ForUser(ctx, account)
	if err != nil || count != nil {
		t.Fatalf("count without an entry for the user = %v (%v), want unknown nil", count, err)
	}

	// An explicit zero entry is a report: the user is known to have none.
	if err := observer.Store.Record("n1", observationSnapshot("n1", credentialID, 0)); err != nil {
		t.Fatal(err)
	}
	count, err = observer.ForUser(ctx, account)
	if err != nil || count == nil || *count != 0 {
		t.Fatalf("count for an explicit zero entry = %v (%v), want 0", count, err)
	}

	// Two nodes reporting the same credential are added up, not deduplicated.
	if err := observer.Store.Record("n1", observationSnapshot("n1", credentialID, 3)); err != nil {
		t.Fatal(err)
	}
	if err := observer.Store.Record("n2", observationSnapshot("n2", credentialID, 4)); err != nil {
		t.Fatal(err)
	}
	count, err = observer.ForUser(ctx, account)
	if err != nil || count == nil || *count != 7 {
		t.Fatalf("count across two nodes = %v (%v), want 7", count, err)
	}
}

// v1.1 retired the per-user connection soft limit (plan C10): the column stays
// for older backups, but even a count above it raises no alert.
func TestObserverNoLongerAlertsOnTheRetiredLimit(t *testing.T) {
	ctx := context.Background()
	_, observer, _, _, credentialID := seedObservedUser(t, 5)
	alerts := 0
	observer.Publish = func(string, any) { alerts++ }
	for _, count := range []uint64{3, 9, 2, 12} {
		if err := observer.Store.Record("n1", observationSnapshot("n1", credentialID, count)); err != nil {
			t.Fatal(err)
		}
		observer.Evaluate(ctx, "n1")
	}
	if alerts != 0 {
		t.Fatalf("the retired limit raised %d alerts", alerts)
	}
}

func TestHandlerObserveFillsConnectionFields(t *testing.T) {
	ctx := context.Background()
	repository, observer, _, account, credentialID := seedObservedUser(t, 5)
	handler := &Handler{Repository: repository, Observations: observer}

	loaded, err := repository.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Unknown: no snapshot covers the user.
	handler.observe(ctx, &loaded)
	if loaded.OnlineConnections != nil {
		t.Fatalf("unknown count = %v, want nil", loaded.OnlineConnections)
	}

	// Known: the count is reported as is.
	if err := observer.Store.Record("n1", observationSnapshot("n1", credentialID, 9)); err != nil {
		t.Fatal(err)
	}
	handler.observe(ctx, &loaded)
	if loaded.OnlineConnections == nil || *loaded.OnlineConnections != 9 {
		t.Fatalf("count = %v, want 9", loaded.OnlineConnections)
	}

	encoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, "\"onlineConnections\":9") {
		t.Fatalf("response omits the online count: %s", encoded)
	}
	for _, retired := range []string{"overLimit", "maxConnections", "maxDevices", "devicesCount"} {
		if strings.Contains(text, "\""+retired+"\"") {
			t.Fatalf("response still carries the retired field %s: %s", retired, encoded)
		}
	}
}

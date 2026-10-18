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

func TestObserverAlertsOncePerUpwardLimitCrossing(t *testing.T) {
	ctx := context.Background()
	_, observer, _, account, credentialID := seedObservedUser(t, 5)
	alerts := 0
	observer.Publish = func(kind string, data any) {
		if kind != "system.alert" {
			t.Fatalf("unexpected event kind %q", kind)
		}
		payload, ok := data.(map[string]any)
		if !ok || payload["alert"] != "connection_limit" || payload["userId"] != account.ID || payload["maxConnections"] != 5 {
			t.Fatalf("unexpected alert payload: %+v", data)
		}
		alerts++
	}
	evaluate := func(count uint64) {
		if err := observer.Store.Record("n1", observationSnapshot("n1", credentialID, count)); err != nil {
			t.Fatal(err)
		}
		observer.Evaluate(ctx, "n1")
	}

	evaluate(3)
	if alerts != 0 {
		t.Fatalf("under-limit count raised %d alerts", alerts)
	}
	evaluate(6)
	if alerts != 1 {
		t.Fatalf("crossing the limit raised %d alerts, want 1", alerts)
	}
	evaluate(6)
	if alerts != 1 {
		t.Fatalf("staying over the limit raised a duplicate alert (%d)", alerts)
	}
	evaluate(2)
	if alerts != 1 {
		t.Fatalf("dropping under the limit raised an alert (%d)", alerts)
	}
	evaluate(7)
	if alerts != 2 {
		t.Fatalf("second crossing raised %d alerts total, want 2", alerts)
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
	if loaded.OnlineConnections != nil || loaded.OverLimit {
		t.Fatalf("unknown count = %v over=%v, want nil/false", loaded.OnlineConnections, loaded.OverLimit)
	}

	// Known and under the limit.
	if err := observer.Store.Record("n1", observationSnapshot("n1", credentialID, 3)); err != nil {
		t.Fatal(err)
	}
	handler.observe(ctx, &loaded)
	if loaded.OnlineConnections == nil || *loaded.OnlineConnections != 3 || loaded.OverLimit {
		t.Fatalf("under count = %v over=%v, want 3/false", loaded.OnlineConnections, loaded.OverLimit)
	}

	// Known and over the limit.
	if err := observer.Store.Record("n1", observationSnapshot("n1", credentialID, 9)); err != nil {
		t.Fatal(err)
	}
	handler.observe(ctx, &loaded)
	if loaded.OnlineConnections == nil || *loaded.OnlineConnections != 9 || !loaded.OverLimit {
		t.Fatalf("over count = %v over=%v, want 9/true", loaded.OnlineConnections, loaded.OverLimit)
	}

	encoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"onlineConnections":9`) || !strings.Contains(string(encoded), `"overLimit":true`) {
		t.Fatalf("response omits connection fields: %s", encoded)
	}
}

func TestObserverIgnoresEvaluateWithoutSnapshot(t *testing.T) {
	ctx := context.Background()
	_, observer, _, _, _ := seedObservedUser(t, 5)
	alerts := 0
	observer.Publish = func(string, any) { alerts++ }
	observer.Evaluate(ctx, "n1") // no snapshot recorded yet
	if alerts != 0 {
		t.Fatalf("evaluate without a snapshot raised %d alerts", alerts)
	}
}

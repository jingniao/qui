package access

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/traffic"
	"testing"
	"time"
)

func TestNodeTrafficIndependentEpochsReceiptsAndNoQuotaDebit(t *testing.T) {
	r, s, record := fixture(t)
	ctx := context.Background()
	account := traffic.Counter{CoreEpoch: "process", Revision: 1, CredentialID: "credential", UserID: record.UserID, DeviceID: record.DeviceID, Protocol: "vless", ObservedAt: s.Now().Add(-time.Minute), UploadBytes: 10, DownloadBytes: 20}
	if err := r.IngestTraffic(ctx, traffic.Batch{ID: "account", NodeID: record.NodeID, Counters: []traffic.Counter{account}}, s.Now()); err != nil {
		t.Fatal(err)
	}
	node := traffic.Counter{Kind: "node", Core: "xray", CoreEpoch: "process.node", Revision: 1, CredentialID: "node", UserID: "node", DeviceID: "node", ObservedAt: account.ObservedAt, UploadBytes: 10, DownloadBytes: 20}
	peer := node
	peer.Core = "sing-box"
	peer.CoreEpoch = "peer.node"
	peer.UploadBytes = 100
	peer.DownloadBytes = 200
	b := traffic.Batch{ID: "node-batch", NodeID: record.NodeID, Counters: []traffic.Counter{node, peer}}
	for i := 0; i < 3; i++ {
		if err := r.IngestTraffic(ctx, b, s.Now()); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := r.AccountSummary(ctx, Filter{NodeID: record.NodeID, From: s.Now().Add(-time.Hour), To: s.Now()})
	if err != nil || summary.UploadBytes != 110 || summary.DownloadBytes != 220 {
		t.Fatal(summary, err)
	}
	// The second core must not retire the first core's high-water mark.
	node.UploadBytes = 15
	b.ID = "next"
	b.Counters = []traffic.Counter{node}
	if err := r.IngestTraffic(ctx, b, s.Now()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := r.Database.QueryRow("SELECT count(*) FROM node_traffic_cursors").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	users := &user.Repository{Database: r.Database}
	if err := users.ApplyTraffic(ctx, b); err != nil {
		t.Fatal("node counter reached quotas", err)
	}
	if err := r.Database.QueryRow("SELECT count(*) FROM user_traffic_cursors").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	altered := b
	altered.Counters = append([]traffic.Counter(nil), b.Counters...)
	altered.Counters[0].UploadBytes++
	if err := r.IngestTraffic(ctx, altered, s.Now()); err == nil {
		t.Fatal("changed receipt accepted")
	}
	b.ID = "forged-core"
	b.Counters[0].Core = "sing-box"
	if err := r.IngestTraffic(ctx, b, s.Now()); err == nil {
		t.Fatal("epoch component changed")
	}
	b.ID = "bad-epoch"
	b.Counters[0].CoreEpoch = "invalid"
	if err := r.IngestTraffic(ctx, b, s.Now()); err == nil {
		t.Fatal("invalid node epoch accepted")
	}
}

// A GOST node reports the same cumulative shape as the other cores. The third core
// kind is a known core for node counters (the shared traffic contract), its epoch is
// independent of the proxy cores' epochs, and its bytes still never reach a quota.
func TestNodeTrafficGostCounterIsANodeTotalOnly(t *testing.T) {
	r, s, record := fixture(t)
	ctx := context.Background()
	gost := traffic.Counter{Kind: "node", Core: "gost", CoreEpoch: "gost-process.node", Revision: 1, CredentialID: "node", UserID: "node", DeviceID: "node", ObservedAt: s.Now().Add(-time.Minute), UploadBytes: 4096, DownloadBytes: 8192}
	batch := traffic.Batch{ID: "gost-node", NodeID: record.NodeID, Counters: []traffic.Counter{gost}}
	if err := r.IngestTraffic(ctx, batch, s.Now()); err != nil {
		t.Fatal(err)
	}
	summary, err := r.AccountSummary(ctx, Filter{NodeID: record.NodeID, From: s.Now().Add(-time.Hour), To: s.Now()})
	if err != nil || summary.UploadBytes != 4096 || summary.DownloadBytes != 8192 {
		t.Fatalf("gost node total: %+v %v", summary, err)
	}
	users := &user.Repository{Database: r.Database}
	if err := users.ApplyTraffic(ctx, batch); err != nil {
		t.Fatal("gost node counter reached quotas", err)
	}
	var count int
	if err := r.Database.QueryRow("SELECT count(*) FROM user_traffic_cursors").Scan(&count); err != nil || count != 0 {
		t.Fatalf("user cursors from a node total: %d %v", count, err)
	}
	// A core kind the platform does not run is still refused.
	forged := gost
	forged.Core, forged.CoreEpoch = "brook", "brook.node"
	if err := r.IngestTraffic(ctx, traffic.Batch{ID: "brook", NodeID: record.NodeID, Counters: []traffic.Counter{forged}}, s.Now()); err == nil {
		t.Fatal("an unknown core kind was accepted")
	}
}

// A forward increment arrives as an account batch plus a node batch in the same
// epoch: the node total is counted once, repeats are idempotent, a second core
// epoch stays independent, and quotas move only by the account delta.
func TestNodeTrafficForwardIncrementCountsNodeTotalOnce(t *testing.T) {
	r, s, record := fixture(t)
	ctx := context.Background()
	users := &user.Repository{Database: r.Database}
	key, err := vault.Open(filepath.Join(t.TempDir(), "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	users.Vault = key
	owner, err := users.Save(ctx, "", user.Input{Username: "forward-user", AllowedProtocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := users.AccessIdentity(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	account := traffic.Counter{CoreEpoch: "process", Revision: 1, CredentialID: "credential", UserID: owner.ID, DeviceID: identity.ID, Protocol: "vless", ObservedAt: now.Add(-time.Minute), UploadBytes: 10, DownloadBytes: 20}
	accountBatch := traffic.Batch{ID: "account", NodeID: record.NodeID, Counters: []traffic.Counter{account}}
	if err := r.IngestTraffic(ctx, accountBatch, now); err != nil {
		t.Fatal(err)
	}
	// The Xray node counter is the account sum plus the forwarded bytes.
	node := traffic.Counter{Kind: "node", Core: "xray", CoreEpoch: "process.node", Revision: 1, CredentialID: "node", UserID: "node", DeviceID: "node", ObservedAt: account.ObservedAt, UploadBytes: 15, DownloadBytes: 45}
	nodeBatch := traffic.Batch{ID: "node", NodeID: record.NodeID, Counters: []traffic.Counter{node}}
	for attempt := 0; attempt < 3; attempt++ {
		if err := r.IngestTraffic(ctx, nodeBatch, now); err != nil {
			t.Fatal(err)
		}
	}
	window := Filter{NodeID: record.NodeID, From: now.Add(-time.Hour), To: now}
	summary, err := r.AccountSummary(ctx, window)
	if err != nil || summary.UploadBytes != 15 || summary.DownloadBytes != 45 || summary.Observations != 1 {
		t.Fatal("forward increment was not counted exactly once", summary, err)
	}
	peer := node
	peer.Core, peer.CoreEpoch, peer.UploadBytes, peer.DownloadBytes = "sing-box", "peer.node", 100, 200
	if err := r.IngestTraffic(ctx, traffic.Batch{ID: "peer", NodeID: record.NodeID, Counters: []traffic.Counter{peer}}, now); err != nil {
		t.Fatal(err)
	}
	if summary, err := r.AccountSummary(ctx, window); err != nil || summary.UploadBytes != 115 || summary.DownloadBytes != 245 {
		t.Fatal("cores shared one node cursor", summary, err)
	}
	if err := users.ApplyTraffic(ctx, accountBatch); err != nil {
		t.Fatal(err)
	}
	if err := users.ApplyTraffic(ctx, nodeBatch); err != nil {
		t.Fatal("node counter reached quotas", err)
	}
	if owner, err := users.Get(ctx, owner.ID); err != nil || owner.UsedTraffic != 30 {
		t.Fatal("forwarded bytes were charged to the user", owner.UsedTraffic, err)
	}
	var cursors int
	if err := r.Database.QueryRow("SELECT count(*) FROM user_traffic_cursors").Scan(&cursors); err != nil || cursors != 1 {
		t.Fatal(cursors, err)
	}
}

package access

import (
	"context"
	"proxycontrol/internal/panel/user"
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

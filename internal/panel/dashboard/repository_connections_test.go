package dashboard

import (
	"context"
	"testing"
	"time"

	"proxycontrol/internal/panel/connections"
	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/traffic"
)

// TestDashboardMeasuredConnectionsComeFromTheLiveStore checks that the measured
// node, connection, user and device counts are read from the in-memory live
// store: a fresh snapshot is counted, and one older than three heartbeat
// intervals is treated as unknown instead of leaving an invented count behind.
func TestDashboardMeasuredConnectionsComeFromTheLiveStore(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	_, database, nodeID := dashboardFixture(t)
	stamp := now.Format("2006-01-02T15:04:05.000000000Z")
	for _, statement := range []string{
		`INSERT INTO users(id,username,display_name,status,max_devices,max_connections,protocols_json,created_at) VALUES('u1','u1','u1','active',2,8,'["socks5"]',?)`,
		`INSERT INTO devices(id,user_id,name,status,created_at) VALUES('d1','u1','d1','active',?)`,
		`INSERT INTO devices(id,user_id,name,status,created_at) VALUES('d2','u1','d2','active',?)`,
		`INSERT INTO credentials(id,user_id,device_id,protocol,encrypted_secret,status,created_at) VALUES('c1','u1','d1','socks5',x'00','active',?)`,
		`INSERT INTO credentials(id,user_id,device_id,protocol,encrypted_secret,status,created_at) VALUES('c2','u1','d2','socks5',x'00','active',?)`,
	} {
		if _, err := database.Exec(statement, stamp); err != nil {
			t.Fatal(err)
		}
	}
	clock := now
	store := connections.NewStore(3 * node.HeartbeatInterval)
	store.Now = func() time.Time { return clock }
	repository := &Repository{Database: database, Connections: store}

	assertCounts := func(step string, nodes int, connectionsCount int64, users, devices int, known bool) {
		t.Helper()
		counts, err := repository.Counts(ctx, now.Add(-time.Hour), now, now)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		if counts.MeasuredNodes != nodes || counts.MeasuredConnections != connectionsCount || counts.MeasuredUsers != users || counts.MeasuredDevices != devices {
			t.Fatalf("%s: measured = %d nodes/%d connections/%d users/%d devices, want %d/%d/%d/%d", step, counts.MeasuredNodes, counts.MeasuredConnections, counts.MeasuredUsers, counts.MeasuredDevices, nodes, connectionsCount, users, devices)
		}
		reported := counts.OnlineUsers != nil && counts.OnlineDevices != nil && counts.ActiveConnections != nil
		if reported != known {
			t.Fatalf("%s: online metrics known = %v, want %v", step, reported, known)
		}
	}

	// No snapshot yet: the measured numbers are unknown, not zero.
	assertCounts("empty store", 0, 0, 0, 0, false)

	if err := store.Record(nodeID, traffic.ConnectionSnapshot{
		NodeID: nodeID, Revision: 1, CoreEpoch: "epoch", ObservedAt: now,
		Scope: traffic.ConnectionScope, Connections: 7,
		Accounts: []traffic.ConnectionAccount{{CredentialID: "c1", Connections: 3}, {CredentialID: "c2", Connections: 4}},
	}); err != nil {
		t.Fatal(err)
	}
	assertCounts("fresh snapshot", 1, 7, 1, 2, true)

	// Exactly three heartbeat intervals later the snapshot is stale.
	clock = now.Add(3 * node.HeartbeatInterval)
	assertCounts("stale snapshot", 0, 0, 0, 0, false)
}

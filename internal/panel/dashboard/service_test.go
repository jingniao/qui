package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil/dbtest"
	contract "proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/traffic"
	"strings"
	"testing"
	"time"
)

func dashboardFixture(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Dashboard node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	settings := &system.Repository{Database: database}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.SaveSetup(ctx, tx, "Test", "Asia/Shanghai"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return &Service{Repository: &Repository{Database: database}, Nodes: nodes, Deployments: &deployment.Repository{Database: database, Vault: key}, Proxies: &proxy.Repository{Database: database, Vault: key}, Settings: settings}, database, n.ID
}

func TestDashboardSystemTimezoneDayAndViewerPrivateFields(t *testing.T) {
	service, database, nodeID := dashboardFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)
	service.Now = func() time.Time { return now }
	records := &access.Repository{Database: database}
	counter := traffic.Counter{CoreEpoch: "epoch", Revision: 1, CredentialID: "credential", UserID: "private-user", DeviceID: "private-device", Protocol: "vless", UploadBytes: 100, DownloadBytes: 200, ObservedAt: time.Date(2026, 10, 1, 16, 30, 0, 0, time.UTC)}
	if err := records.IngestTraffic(ctx, traffic.Batch{ID: "today", NodeID: nodeID, Counters: []traffic.Counter{counter}}, now); err != nil {
		t.Fatal(err)
	}
	record := contract.Rollup{Bucket: counter.ObservedAt, NodeID: nodeID, Revision: 1, InboundProtocol: "vless", Domain: "private.example", TargetPort: 443, Network: "tcp", Action: "Block", Allowed: false, Connections: 3}
	if err := records.Ingest(ctx, contract.Batch{ID: "blocked", NodeID: nodeID, Records: []contract.Rollup{record}}, now); err != nil {
		t.Fatal(err)
	}
	view, err := service.Read(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if !view.DayStart.Equal(time.Date(2026, 10, 1, 16, 0, 0, 0, time.UTC)) || view.Counts.TodayUpload != 100 || view.Counts.TodayDownload != 200 || view.Counts.TodayBlocked != 3 {
		t.Fatal("system timezone day ignored", view)
	}
	if len(view.RecentBlocked) != 1 || view.RecentBlocked[0].Domain != "private.example" {
		t.Fatal("actual private records missing")
	}
	if view.Counts.OnlineUsers != nil || view.Counts.ActiveConnections != nil {
		t.Fatal("unknown metrics fabricated")
	}
	viewer, err := service.Read(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(viewer)
	for _, private := range []string{"private.example", "private-user", "private-device", "recentAccess", "recentBlocked", "revisions", "chains"} {
		if strings.Contains(string(data), private) {
			t.Fatal("Viewer dashboard leaked private field", private)
		}
	}
}

func TestDashboardReadDoesNotWriteAndStaleHeartbeatsAreOffline(t *testing.T) {
	service, database, nodeID := dashboardFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	service.Now = func() time.Time { return now }
	if _, err := database.Exec("UPDATE nodes SET status='online',last_heartbeat=? WHERE id=?", now.Add(-time.Minute).Format("2006-01-02T15:04:05.000000000Z"), nodeID); err != nil {
		t.Fatal(err)
	}
	var before int64
	database.QueryRow("SELECT total_changes()").Scan(&before)
	view, err := service.Read(ctx, true)
	if err != nil || view.Counts.OnlineNodes != 0 || view.Counts.OfflineNodes != 1 || view.Nodes[0].Status != "offline" {
		t.Fatal("stale heartbeat remains online", view, err)
	}
	var after int64
	database.QueryRow("SELECT total_changes()").Scan(&after)
	if before != after {
		t.Fatal("dashboard query modified business data")
	}
	if _, err := database.Exec("UPDATE nodes SET status='degraded' WHERE id=?", nodeID); err != nil {
		t.Fatal(err)
	}
	view, err = service.Read(ctx, true)
	if err != nil || view.Counts.OfflineNodes != 1 || view.Counts.AbnormalNodes != 0 || view.Nodes[0].Status != "offline" {
		t.Fatal("stale degraded heartbeat counted as a current core fault", view, err)
	}
}

func TestDashboardNodeTrafficTodayAndMonth(t *testing.T) {
	service, database, nodeID := dashboardFixture(t)
	ctx := context.Background()
	// 2026-10-15 09:00 in Asia/Shanghai: the local day starts at 10-14T16:00Z
	// and the local month at 09-30T16:00Z.
	now := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	service.Now = func() time.Time { return now }
	idle, _, _, err := (&node.Repository{Database: database}).CreateEnrollment(ctx, "Idle node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	records := &access.Repository{Database: database}
	for id, at := range map[string]time.Time{
		"today":      time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC),
		"this-month": time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC),
		"last-month": time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC),
	} {
		counter := traffic.Counter{CoreEpoch: "epoch-" + id, Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "vless", UploadBytes: 10, DownloadBytes: 20, ObservedAt: at}
		if err := records.IngestTraffic(ctx, traffic.Batch{ID: id, NodeID: nodeID, Counters: []traffic.Counter{counter}}, now); err != nil {
			t.Fatal(err)
		}
	}
	view, err := service.Read(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	byNode := map[string]NodeTraffic{}
	for _, item := range view.NodeTraffic {
		byNode[item.NodeID] = item
	}
	busy, quiet := byNode[nodeID], byNode[idle.ID]
	if len(view.NodeTraffic) != 2 || busy.TodayUpload != 10 || busy.TodayDownload != 20 || busy.MonthUpload != 20 || busy.MonthDownload != 40 || !busy.Measured {
		t.Fatalf("busy node traffic = %+v (%+v)", busy, view.NodeTraffic)
	}
	if busy.TodayUpload != view.Counts.TodayUpload || busy.TodayDownload != view.Counts.TodayDownload {
		t.Fatalf("per-node today %+v does not add up to the total %+v", busy, view.Counts)
	}
	if quiet.Measured || quiet.MonthUpload != 0 || quiet.TodayDownload != 0 {
		t.Fatalf("idle node traffic = %+v", quiet)
	}
}

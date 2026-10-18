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
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	contract "proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/traffic"
	"strings"
	"testing"
	"time"
)

func dashboardFixture(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
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

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
	for _, c := range []struct {
		name, status, heartbeat, capabilities, display string
		age                                            time.Duration
		duration                                       int
		enabled, maintenance                           bool
		online, offline, abnormal                      int
	}{
		{name: "waiting for recovery", status: "online", age: time.Minute, duration: 3, enabled: true, display: "reconnecting"},
		{name: "freshness boundary", status: "online", age: 45 * time.Second, duration: 3, enabled: true, display: "reconnecting"},
		{name: "future heartbeat", status: "online", age: -6 * time.Second, duration: 3, enabled: true, offline: 1, display: "offline"},
		{name: "duration boundary", status: "online", age: 3 * time.Minute, duration: 3, enabled: true, offline: 1, display: "offline"},
		{name: "zero duration", status: "online", age: time.Minute, duration: 0, enabled: true, offline: 1, display: "offline"},
		{name: "fresh with zero duration", status: "online", duration: 0, enabled: true, online: 1, display: "online"},
		{name: "disconnected with fresh heartbeat", status: "offline", age: time.Second, duration: 3, enabled: true, display: "reconnecting"},
		{name: "degraded waiting", status: "degraded", age: time.Minute, duration: 3, enabled: true, display: "reconnecting"},
		{name: "degraded offline", status: "degraded", age: 3 * time.Minute, duration: 3, enabled: true, offline: 1, display: "offline"},
		{name: "degraded online", status: "degraded", duration: 3, enabled: true, online: 1, abnormal: 1, display: "degraded"},
		{name: "core fault online", status: "degraded", heartbeat: `{"core_healthy":false}`, duration: 3, enabled: true, online: 1, abnormal: 1, display: "core_error"},
		{name: "per-core fault online", status: "online", heartbeat: `{"cores":[{"name":"xray","healthy":false}]}`, duration: 3, enabled: true, online: 1, abnormal: 1, display: "core_error"},
		{name: "single-core scalar health overrides core list", status: "online", heartbeat: `{"core_healthy":true,"cores":[{"name":"xray","healthy":false}]}`, duration: 3, enabled: true, online: 1, display: "online"},
		{name: "single-core capability health stays unknown", status: "online", capabilities: `{"core_name":"xray","cores":[{"name":"xray","healthy":false}]}`, duration: 3, enabled: true, online: 1, display: "online"},
		{name: "single-core ignores secondary capability", status: "online", capabilities: `{"core_name":"xray","cores":[{"name":"xray"},{"name":"sing-box","healthy":false}]}`, duration: 3, enabled: true, online: 1, display: "online"},
		{name: "multi-core retains capability health", status: "online", capabilities: `{"core_name":"xray","multi_core_supported":true,"cores":[{"name":"xray","healthy":false}]}`, duration: 3, enabled: true, online: 1, abnormal: 1, display: "core_error"},
		{name: "error retains explicit state", status: "error", age: 3 * time.Minute, duration: 3, enabled: true, abnormal: 1, display: "error"},
		{name: "maintenance", status: "online", age: 3 * time.Minute, duration: 3, enabled: true, maintenance: true, display: "maintenance"},
		{name: "disabled", status: "online", age: 3 * time.Minute, duration: 3, display: "disabled"},
		{name: "pending", status: "pending", age: 3 * time.Minute, duration: 3, enabled: true, display: "pending"},
	} {
		heartbeat := c.heartbeat
		if heartbeat == "" {
			heartbeat = "{}"
		}
		capabilities := c.capabilities
		if capabilities == "" {
			capabilities = `{"core_name":"xray"}`
		}
		if _, err := database.Exec("UPDATE notification_rules SET duration_minutes=? WHERE kind='node_offline'", c.duration); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec("UPDATE nodes SET status=?,last_heartbeat=?,enabled=?,maintenance=?,heartbeat_json=?,capabilities_json=? WHERE id=?", c.status, now.Add(-c.age).Format("2006-01-02T15:04:05.000000000Z"), c.enabled, c.maintenance, heartbeat, capabilities, nodeID); err != nil {
			t.Fatal(err)
		}
		var before int64
		if err := database.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
			t.Fatal(err)
		}
		view, err := service.Read(ctx, true)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if view.Counts.OnlineNodes != c.online || view.Counts.OfflineNodes != c.offline || view.Counts.AbnormalNodes != c.abnormal {
			t.Fatalf("%s: counts = %+v", c.name, view.Counts)
		}
		if got := node.DisplayStatus(view.Nodes[0], now); got != c.display {
			t.Fatalf("%s: display = %s, want %s", c.name, got, c.display)
		}
		if c.age >= time.Minute && (c.status == "online" || c.status == "degraded") && view.Nodes[0].Status != "offline" {
			t.Fatalf("%s: raw control status = %s, want offline", c.name, view.Nodes[0].Status)
		}
		var after int64
		if err := database.QueryRow("SELECT total_changes()").Scan(&after); err != nil {
			t.Fatal(err)
		}
		if before != after {
			t.Fatal("dashboard query modified business data")
		}
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

func TestDashboardBaseIndependentOfStatisticsAndViewerPrivacy(t *testing.T) {
	service, database, _ := dashboardFixture(t)
	if _, err := database.Exec("DROP VIEW node_traffic_samples"); err != nil {
		t.Fatal(err)
	}
	view, err := service.Base(context.Background(), false)
	if err != nil || view.Counts.TotalNodes != 1 || len(view.Nodes) != 1 {
		t.Fatal(view, err)
	}
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"trend", "nodeTraffic", "todayUpload", "todayDownload", "trafficObservations", "recentAccess", "revisions"} {
		if strings.Contains(string(data), `"`+key+`"`) {
			t.Fatalf("base contains %s: %s", key, data)
		}
	}
}

func TestPublishedDashboardIncludesCurrentSecondWithoutMachineStatistics(t *testing.T) {
	service, database, id := dashboardFixture(t)
	now := time.Date(2026, 11, 18, 8, 15, 0, 0, time.UTC)
	service.Now = func() time.Time { return now }
	if _, err := database.Exec("INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','current',?,5,3)", id, now.Add(500*time.Millisecond).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("ALTER TABLE node_machine_traffic_daily RENAME TO unavailable_machine"); err != nil {
		t.Fatal(err)
	}

	v, err := service.Read(context.Background(), false)
	if err != nil || v.Counts.TodayUpload != 5 || v.Counts.TodayDownload != 3 || v.Counts.TrafficObservations != 1 || len(v.Trend) != 1 || v.Trend[0].Upload != 5 || len(v.NodeTraffic) != 1 || v.NodeTraffic[0].MonthUpload != 5 {
		t.Fatal(v, err)
	}
}

func TestDashboardBaseSkippedMidnightPreservesPublishedBoundary(t *testing.T) {
	service, database, id := dashboardFixture(t)
	if _, err := database.Exec(`UPDATE system_settings SET value_json='"America/Santiago"' WHERE key='timezone'`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	service.Now = func() time.Time { return now }
	ctx := context.Background()
	previousEvening := time.Date(2026, 9, 6, 3, 30, 0, 0, time.UTC)
	record := contract.Rollup{Bucket: previousEvening, NodeID: id, Revision: 1, InboundProtocol: "vless", Domain: "example.org", TargetPort: 443, Network: "tcp", Action: "Block", Allowed: false, Connections: 2}
	if err := (&access.Repository{Database: database}).Ingest(ctx, contract.Batch{ID: "previous-evening", NodeID: id, Records: []contract.Rollup{record}}, now); err != nil {
		t.Fatal(err)
	}
	base, err := service.Base(ctx, false)
	if err != nil || !base.DayStart.Equal(time.Date(2026, 9, 6, 4, 0, 0, 0, time.UTC)) || base.Counts.TodayBlocked != 0 {
		t.Errorf("new endpoint included previous local day: %v %d %v", base.DayStart, base.Counts.TodayBlocked, err)
	}
	old, err := service.Read(ctx, false)
	if err != nil || !old.DayStart.Equal(time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)) || old.Counts.TodayBlocked != 2 {
		t.Fatalf("published endpoint changed: %v %d %v", old.DayStart, old.Counts.TodayBlocked, err)
	}
}

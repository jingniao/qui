package access_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/db"
	contract "proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

// This test is an external package so it can import the central system
// repository without an import cycle (system depends on access).
type retentionSnapshots struct{ state desiredstate.State }

func (s retentionSnapshots) AppliedState(_ context.Context, nodeID string, revision int64) (desiredstate.State, error) {
	if nodeID != s.state.NodeID || revision != s.state.Revision {
		return desiredstate.State{}, errors.New("not applied")
	}
	return s.state, nil
}

func retentionFixture(t *testing.T) (*access.Repository, *access.Service, contract.Rollup) {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Access", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	state := desiredstate.State{NodeID: n.ID, Revision: 1, Credentials: []desiredstate.Credential{{ID: "credential", Protocol: "vless", UserID: "user", DeviceID: "device", Secret: "unused-test-secret"}}, Inbounds: []desiredstate.Inbound{{ID: "in", Protocol: "vless", Enabled: true, CredentialIDs: []string{"credential"}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	r := &access.Repository{Database: database}
	now := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	service := &access.Service{Repository: r, Snapshots: retentionSnapshots{state: state}, Now: func() time.Time { return now }}
	record := contract.Rollup{Bucket: now.Add(-time.Minute), NodeID: n.ID, Revision: 1, UserID: "user", DeviceID: "device", InboundProtocol: "vless", Domain: "private.example", TargetPort: 443, Network: "tcp", Action: "Direct", Allowed: true, Connections: 50, TrafficKnown: true, UploadBytes: 2000, DownloadBytes: 9000, OutboundID: "direct"}
	return r, service, record
}

func countRows(t *testing.T, r *access.Repository, table string) int {
	t.Helper()
	var count int
	if err := r.Database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestConfiguredRetentionControlsLateAccessAndNativeCounters(t *testing.T) {
	r, s, record := retentionFixture(t)
	ctx := context.Background()
	settings := &system.Repository{Database: r.Database}
	r.Settings = settings
	if _, err := settings.SaveRetention(ctx, system.RetentionInput{Policy: contract.Retention{AllowedDays: 7, BlockedDays: 30, HourlyDays: 1}}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	record.Bucket = s.Now().AddDate(0, 0, -10)
	allowed := contract.Batch{ID: "old-allowed", NodeID: record.NodeID, Records: []contract.Rollup{record}}
	if err := r.Ingest(ctx, allowed, s.Now()); err != nil {
		t.Fatal(err)
	}
	if countRows(t, r, "access_rollups") != 0 || countRows(t, r, "traffic_hourly") != 0 || countRows(t, r, "traffic_daily") != 1 {
		t.Fatal("late expired access or hourly data resurrected")
	}
	blocked := record
	blocked.Allowed = false
	blocked.Action = "Block"
	blocked.UploadBytes = 0
	blocked.DownloadBytes = 0
	if err := r.Ingest(ctx, contract.Batch{ID: "old-blocked", NodeID: record.NodeID, Records: []contract.Rollup{blocked}}, s.Now()); err != nil {
		t.Fatal(err)
	}
	if countRows(t, r, "access_rollups") != 1 {
		t.Fatal("separate blocked retention ignored")
	}
	// Access can be retained longer than hourly counters; that must not recreate
	// expired hourly rows when the same late record is otherwise allowed.
	recent := record
	recent.Bucket = s.Now().AddDate(0, 0, -2)
	if err := r.Ingest(ctx, contract.Batch{ID: "recent-access", NodeID: record.NodeID, Records: []contract.Rollup{recent}}, s.Now()); err != nil {
		t.Fatal(err)
	}
	if countRows(t, r, "access_rollups") != 2 || countRows(t, r, "traffic_hourly") != 0 {
		t.Fatal("access lifetime overrode hourly privacy window")
	}
	counter := traffic.Counter{CoreEpoch: "retention-epoch", Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "vless", UploadBytes: 100, DownloadBytes: 200, ObservedAt: s.Now().AddDate(0, 0, -2)}
	if err := r.IngestTraffic(ctx, traffic.Batch{ID: "late-native", NodeID: record.NodeID, Counters: []traffic.Counter{counter}}, s.Now()); err != nil {
		t.Fatal(err)
	}
	if countRows(t, r, "account_traffic_hourly") != 0 || countRows(t, r, "account_traffic_samples") != 0 || countRows(t, r, "account_traffic_daily") != 1 || countRows(t, r, "traffic_cursors") != 1 {
		t.Fatal("native counters ignored configured retention or lost replay cursor")
	}
	if _, err := settings.SaveRetention(ctx, system.RetentionInput{Policy: contract.Retention{AllowedDays: 1, BlockedDays: 1, HourlyDays: 1}, ExpectedRevision: 1}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	if err := r.PruneConfigured(ctx, s.Now()); err != nil {
		t.Fatal(err)
	}
	if countRows(t, r, "access_rollups") != 0 || countRows(t, r, "access_batches") != 3 || countRows(t, r, "traffic_batches") != 1 || countRows(t, r, "traffic_daily") != 2 || countRows(t, r, "account_traffic_daily") != 1 {
		t.Fatal("cleanup lost daily data or replay protection")
	}
	if err := r.Ingest(ctx, allowed, s.Now()); err != nil {
		t.Fatal(err)
	}
	if countRows(t, r, "access_rollups") != 0 {
		t.Fatal("cleaned batch replay recreated records")
	}
}
func TestHourlyRetentionBoundaryCannotRecreateExpiredBucket(t *testing.T) {
	r, s, record := retentionFixture(t)
	ctx := context.Background()
	settings := &system.Repository{Database: r.Database}
	r.Settings = settings
	if _, err := settings.SaveRetention(ctx, system.RetentionInput{Policy: contract.Retention{AllowedDays: 7, BlockedDays: 30, HourlyDays: 1}}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	now := s.Now().Add(30 * time.Minute)
	record.Bucket = now.AddDate(0, 0, -1).Add(time.Minute)
	if err := r.Ingest(ctx, contract.Batch{ID: "boundary-access", NodeID: record.NodeID, Records: []contract.Rollup{record}}, now); err != nil {
		t.Fatal(err)
	}
	counter := traffic.Counter{CoreEpoch: "boundary-epoch", Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "vless", UploadBytes: 100, ObservedAt: record.Bucket}
	if err := r.IngestTraffic(ctx, traffic.Batch{ID: "boundary-native", NodeID: record.NodeID, Counters: []traffic.Counter{counter}}, now); err != nil {
		t.Fatal(err)
	}
	if countRows(t, r, "traffic_hourly") != 0 || countRows(t, r, "account_traffic_hourly") != 0 || countRows(t, r, "account_traffic_samples") != 1 {
		t.Fatal("late record recreated an expired coarse hour bucket")
	}
}
func TestInvalidRetentionPreventsCleanupAndPartialIngestion(t *testing.T) {
	r, s, record := retentionFixture(t)
	ctx := context.Background()
	settings := &system.Repository{Database: r.Database}
	r.Settings = settings
	if err := r.Ingest(ctx, contract.Batch{ID: "initial", NodeID: record.NodeID, Records: []contract.Rollup{record}}, s.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec("INSERT INTO system_settings(key,value_json) VALUES('access_retention','invalid')"); err != nil {
		t.Fatal(err)
	}
	if err := r.PruneConfigured(ctx, s.Now().AddDate(0, 0, 100)); err == nil {
		t.Fatal("corrupt settings used destructive fallback")
	}
	if countRows(t, r, "access_rollups") != 1 {
		t.Fatal("corrupt policy deleted data")
	}
	record.Bucket = record.Bucket.Add(time.Minute)
	if err := r.Ingest(ctx, contract.Batch{ID: "rejected", NodeID: record.NodeID, Records: []contract.Rollup{record}}, s.Now()); err == nil {
		t.Fatal("corrupt policy accepted new batch")
	}
	if countRows(t, r, "access_rollups") != 1 || countRows(t, r, "access_batches") != 1 {
		t.Fatal("rejected policy left partial ingestion")
	}
}

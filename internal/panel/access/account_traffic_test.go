package access

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
	"testing"
	"time"
)

type failTraffic struct {
	delegate *user.Repository
	fail     bool
}

func TestSameTimestampCumulativeDeltasMergeAndReplayWithoutDoubleCounting(t *testing.T) {
	r, s, record := fixture(t)
	ctx := context.Background()
	counter := traffic.Counter{CoreEpoch: "same-clock-epoch", Revision: 1, CredentialID: "credential", UserID: record.UserID, DeviceID: record.DeviceID, Protocol: "vless", ObservedAt: s.Now().Add(-time.Minute), UploadBytes: 12, DownloadBytes: 24}
	submit := func(id string, counter traffic.Counter) error {
		return r.IngestTraffic(ctx, traffic.Batch{ID: id, NodeID: record.NodeID, Counters: []traffic.Counter{counter}}, s.Now())
	}
	if err := submit("first", counter); err != nil {
		t.Fatal(err)
	}
	counter.UploadBytes = 48
	counter.DownloadBytes = 96
	for _, id := range []string{"second", "second", "same-values-new-id"} {
		if err := submit(id, counter); err != nil {
			t.Fatal("same-time increment or replay rejected", err)
		}
	}
	older := counter
	older.UploadBytes = 12
	older.DownloadBytes = 24
	if err := submit("late-old-same-time", older); err != nil {
		t.Fatal(err)
	}
	var count int
	var upload, download int64
	if err := r.Database.QueryRow("SELECT count(*),sum(upload_bytes),sum(download_bytes) FROM account_traffic_samples").Scan(&count, &upload, &download); err != nil || count != 1 || upload != 48 || download != 96 {
		t.Fatal("same-time delta double-counted or lost", count, upload, download, err)
	}
	summary, err := r.AccountSummary(ctx, Filter{From: s.Now().Add(-time.Hour), To: s.Now()})
	if err != nil || summary.UploadBytes != 48 || summary.DownloadBytes != 96 || summary.Observations != 1 {
		t.Fatal(summary, err)
	}
	if _, err := r.Database.Exec("UPDATE account_traffic_samples SET upload_bytes=?", int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	counter.UploadBytes++
	if err := submit("overflow-same-time", counter); err == nil {
		t.Fatal("sample overflow admitted")
	}
	var cursor int64
	if err := r.Database.QueryRow("SELECT upload_bytes FROM traffic_cursors").Scan(&cursor); err != nil || cursor != 48 {
		t.Fatal("sample failure advanced cursor", cursor, err)
	}
	var receipts int
	if err := r.Database.QueryRow("SELECT count(*) FROM traffic_batches WHERE batch_id='overflow-same-time'").Scan(&receipts); err != nil || receipts != 0 {
		t.Fatal("sample failure acknowledged receipt", receipts, err)
	}
}

func (f *failTraffic) ApplyTraffic(ctx context.Context, batch traffic.Batch) error {
	if f.fail {
		return errors.New("user write failed")
	}
	return f.delegate.ApplyTraffic(ctx, batch)
}

func TestCumulativeTrafficEpochDedupOutOfOrderAndCrossModuleRepair(t *testing.T) {
	r, s, record := fixture(t)
	ctx := context.Background()
	key, err := vault.Open(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: r.Database, Vault: key}
	u, err := users.Save(ctx, "", user.Input{Username: "metered_user", Status: "active", MaxDevices: 1, MaxConnections: 8, AllowedProtocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Device", Protocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	state := s.Snapshots.(snapshots).state
	state.Credentials[0].ID = device.Credentials[0].ID
	state.Credentials[0].UserID = u.ID
	state.Credentials[0].DeviceID = device.ID
	state.Inbounds[0].CredentialIDs = []string{device.Credentials[0].ID}
	s.Snapshots = snapshots{state: state}
	fault := &failTraffic{delegate: users, fail: true}
	s.Users = fault
	counter := traffic.Counter{CoreEpoch: "epoch-one", Revision: 1, CredentialID: device.Credentials[0].ID, UserID: u.ID, DeviceID: device.ID, Protocol: "vless", UploadBytes: 100, DownloadBytes: 1000, ObservedAt: s.Now().Add(-time.Minute)}
	submit := func(id string, counter traffic.Counter) error {
		t.Helper()
		batch := traffic.Batch{ID: id, NodeID: record.NodeID, Counters: []traffic.Counter{counter}}
		data, _ := json.Marshal(batch)
		_, err := s.RecordTraffic(ctx, record.NodeID, &pb.TrafficBatch{BatchJson: data}, true)
		return err
	}
	if err := submit("first", counter); err == nil {
		t.Fatal("cross-module failure was acknowledged")
	}
	fault.fail = false
	if err := submit("first", counter); err != nil {
		t.Fatal("receipt replay did not repair user state", err)
	}
	if err := submit("first", counter); err != nil {
		t.Fatal(err)
	}
	counter.UploadBytes = 200
	counter.DownloadBytes = 2000
	counter.ObservedAt = counter.ObservedAt.Add(5 * time.Second)
	if err := submit("second", counter); err != nil {
		t.Fatal(err)
	}
	if err := submit("same-values", counter); err != nil {
		t.Fatal(err)
	}
	old := counter
	old.UploadBytes = 150
	old.DownloadBytes = 1500
	old.ObservedAt = counter.ObservedAt.Add(-time.Second)
	if err := submit("late-old", old); err != nil {
		t.Fatal("out-of-order counter not harmless", err)
	}
	restarted := counter
	restarted.CoreEpoch = "epoch-two"
	restarted.UploadBytes = 10
	restarted.DownloadBytes = 20
	if err := submit("new-epoch", restarted); err != nil {
		t.Fatal(err)
	}
	updated, err := users.Get(ctx, u.ID)
	if err != nil || updated.UsedTraffic != 2230 {
		t.Fatal("user traffic duplicated or reset", updated.UsedTraffic, err)
	}
	updatedDevice, err := users.Device(ctx, device.ID)
	if err != nil || updatedDevice.UsedTraffic != 2230 {
		t.Fatal("device traffic wrong", updatedDevice.UsedTraffic, err)
	}
	var upload, download int64
	r.Database.QueryRow("SELECT sum(upload_bytes),sum(download_bytes) FROM account_traffic_daily").Scan(&upload, &download)
	if upload != 210 || download != 2020 {
		t.Fatal("account totals wrong", upload, download)
	}
	summary, err := r.AccountSummary(ctx, Filter{From: s.Now().Add(-time.Hour), To: s.Now().Add(time.Hour)})
	if err != nil || summary.UploadBytes != 210 || summary.DownloadBytes != 2020 {
		t.Fatal("account query did not reflect deduplicated totals", summary, err)
	}
	changed := counter
	changed.UserID = "different-user"
	changed.UploadBytes = 1000
	changed.ObservedAt = s.Now()
	if err := r.IngestTraffic(ctx, traffic.Batch{ID: "changed-identity", NodeID: record.NodeID, Counters: []traffic.Counter{changed}}, s.Now()); err == nil {
		t.Fatal("counter identity moved to another user")
	}
	var domains int
	r.Database.QueryRow("SELECT count(*) FROM access_rollups").Scan(&domains)
	if domains != 0 {
		t.Fatal("account totals invented domain attribution")
	}
	counter.UploadBytes = 1
	counter.ObservedAt = s.Now()
	if err := submit("same-epoch-reset", counter); err == nil {
		t.Fatal("new counter reset accepted within same epoch")
	}
}

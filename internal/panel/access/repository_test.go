package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	contract "proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"sync"
	"testing"
	"time"
)

type snapshots struct{ state desiredstate.State }

func (s snapshots) AppliedState(_ context.Context, nodeID string, revision int64) (desiredstate.State, error) {
	if nodeID != s.state.NodeID || revision != s.state.Revision {
		return desiredstate.State{}, errors.New("not applied")
	}
	return s.state, nil
}

func fixture(t *testing.T) (*Repository, *Service, contract.Rollup) {
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
	r := &Repository{Database: database}
	now := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	service := &Service{Repository: r, Snapshots: snapshots{state: state}, Now: func() time.Time { return now }}
	record := contract.Rollup{Bucket: now.Add(-time.Minute), NodeID: n.ID, Revision: 1, UserID: "user", DeviceID: "device", InboundProtocol: "vless", Domain: "private.example", TargetPort: 443, Network: "tcp", Action: "Direct", Allowed: true, Connections: 50, TrafficKnown: true, UploadBytes: 2000, DownloadBytes: 9000, OutboundID: "direct"}
	return r, service, record
}

func submit(t *testing.T, s *Service, batch contract.Batch, allowNew bool) error {
	t.Helper()
	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordAccess(context.Background(), batch.NodeID, &pb.AccessBatch{BatchJson: data}, allowNew)
	return err
}

func TestConcurrentBatchDeduplicationAndConflictDoesNotAddTraffic(t *testing.T) {
	r, s, record := fixture(t)
	ctx := context.Background()
	batch := contract.Batch{ID: "batch", NodeID: record.NodeID, Records: []contract.Rollup{record}}
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- r.Ingest(ctx, batch, s.Now()) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	filter := Filter{From: record.Bucket.Add(-time.Hour), To: record.Bucket.Add(time.Hour)}
	summary, err := r.Summary(ctx, filter)
	if err != nil || summary.Connections != 50 || summary.UploadBytes != 2000 || summary.DownloadBytes != 9000 {
		t.Fatal("duplicate batch changed counters", summary, err)
	}
	batch.Records = append([]contract.Rollup(nil), batch.Records...)
	batch.Records[0].Connections++
	if err := submit(t, s, batch, true); !errors.Is(err, ErrConflict) {
		t.Fatal("changed batch ID accepted", err)
	}
	var daily int64
	if err := r.Database.QueryRow("SELECT sum(download_bytes) FROM traffic_daily").Scan(&daily); err != nil || daily != 9000 {
		t.Fatal("daily counters duplicated", daily, err)
	}
}

func TestAccessSnapshotAttributionAndStaleSequenceValidation(t *testing.T) {
	r, s, record := fixture(t)
	batch := contract.Batch{ID: "good", NodeID: record.NodeID, Records: []contract.Rollup{record}}
	if err := submit(t, s, batch, false); err == nil {
		t.Fatal("stale sequence created a batch")
	}
	if err := submit(t, s, batch, true); err != nil {
		t.Fatal(err)
	}
	if err := submit(t, s, batch, false); err != nil {
		t.Fatal("duplicate receipt was not accepted", err)
	}
	for index, change := range []func(*contract.Rollup){func(r *contract.Rollup) { r.UserID = "other-user" }, func(r *contract.Rollup) { r.DeviceID = "other-device" }, func(r *contract.Rollup) { r.Revision = 2 }, func(r *contract.Rollup) { r.RuleID = "missing" }, func(r *contract.Rollup) { r.OutboundID = "missing" }, func(r *contract.Rollup) { r.Domain = "https://private.example/password" }, func(r *contract.Rollup) { r.Bucket = s.Now().Add(time.Hour) }, func(r *contract.Rollup) { r.EgressKnown = true; r.FinalEgressNodeID = "another-exit" }} {
		bad := record
		change(&bad)
		badBatch := contract.Batch{ID: []string{"bad-user", "bad-device", "bad-revision", "bad-rule", "bad-outbound", "bad-content", "bad-clock", "bad-exit"}[index], NodeID: record.NodeID, Records: []contract.Rollup{bad}}
		if err := submit(t, s, badBatch, true); err == nil {
			t.Fatal("invalid access metadata accepted", index)
		}
	}
	state := s.Snapshots.(snapshots).state
	state.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
	state.Inbounds[0].CredentialIDs = nil
	s.Snapshots = snapshots{state: state}
	batch.ID = "unbound-credential"
	if err := submit(t, s, batch, true); err == nil {
		t.Fatal("unbound credential attributed to inbound")
	}
	var count int
	r.Database.QueryRow("SELECT count(*) FROM access_batches").Scan(&count)
	if count != 1 {
		t.Fatal("rejected metadata left receipts", count)
	}
}

// TestNullBytesRoundTripAndUnknownTrafficSerialisesAsNull proves the nullable
// per-domain byte columns of migration 0106 (plan D4): an unknown measurement is
// stored as NULL and read back as null in the API JSON, while a measured value
// keeps its number. An unknown measurement is never an invented zero.
func TestNullBytesRoundTripAndUnknownTrafficSerialisesAsNull(t *testing.T) {
	r, s, record := fixture(t)
	ctx := context.Background()
	unknown := record
	unknown.Domain = "unknown.example"
	unknown.UploadBytes, unknown.DownloadBytes, unknown.TrafficKnown = 0, 0, false
	for index, item := range []contract.Rollup{unknown, record} {
		if err := r.Ingest(ctx, contract.Batch{ID: []string{"unknown-bytes", "measured-bytes"}[index], NodeID: record.NodeID, Records: []contract.Rollup{item}}, s.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var upload, download sql.NullInt64
	if err := r.Database.QueryRow("SELECT upload_bytes,download_bytes FROM access_rollups WHERE domain='unknown.example'").Scan(&upload, &download); err != nil || upload.Valid || download.Valid {
		t.Fatalf("unknown measurement stored a byte value: %v %v %v", upload, download, err)
	}
	if err := r.Database.QueryRow("SELECT upload_bytes,download_bytes FROM access_rollups WHERE domain='private.example'").Scan(&upload, &download); err != nil || !upload.Valid || upload.Int64 != 2000 || !download.Valid || download.Int64 != 9000 {
		t.Fatalf("measured bytes were not kept: %v %v %v", upload, download, err)
	}
	items, _, err := r.List(ctx, Filter{From: s.Now().AddDate(0, 0, -1), To: s.Now()}, 1, 20)
	if err != nil || len(items) != 2 {
		t.Fatalf("read back %d access records: %v", len(items), err)
	}
	for _, item := range items {
		switch item.Domain {
		case "unknown.example":
			if item.UploadBytes != nil || item.DownloadBytes != nil || item.TrafficKnown {
				t.Fatalf("unknown measurement read back as a value: %+v", item)
			}
		case "private.example":
			if item.UploadBytes == nil || *item.UploadBytes != 2000 || item.DownloadBytes == nil || *item.DownloadBytes != 9000 {
				t.Fatalf("measured bytes read back wrong: %+v", item)
			}
		default:
			t.Fatalf("unexpected access record %q", item.Domain)
		}
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"domain":"unknown.example"`) || !strings.Contains(string(encoded), `"upload_bytes":null`) || !strings.Contains(string(encoded), `"download_bytes":null`) {
		t.Fatalf("unknown bytes are not serialised as null: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"upload_bytes":2000`) || !strings.Contains(string(encoded), `"download_bytes":9000`) {
		t.Fatalf("measured bytes are not serialised as numbers: %s", encoded)
	}
	domains, _, err := r.Domains(ctx, Filter{From: s.Now().AddDate(0, 0, -1), To: s.Now()}, 1, 20)
	if err != nil || len(domains) != 2 {
		t.Fatalf("read back %d domains: %v", len(domains), err)
	}
	for _, item := range domains {
		if item.Domain == "unknown.example" && (item.UploadBytes != nil || item.DownloadBytes != nil) {
			t.Fatalf("unknown domain summary invented bytes: %+v", item)
		}
		if item.Domain == "private.example" && (item.UploadBytes == nil || *item.UploadBytes != 2000) {
			t.Fatalf("measured domain summary lost bytes: %+v", item)
		}
	}
}

func TestRetentionPreservesDailyAndReceiptsAndOverflowRollsBack(t *testing.T) {
	r, s, record := fixture(t)
	ctx := context.Background()
	allowed := record
	allowed.Bucket = s.Now().AddDate(0, 0, -31)
	blocked := allowed
	blocked.Allowed = false
	blocked.Action = "Block"
	blocked.UploadBytes = 0
	blocked.DownloadBytes = 0
	blocked.TrafficKnown = false
	oldBlocked := blocked
	oldBlocked.Bucket = s.Now().AddDate(0, 0, -95)
	for index, item := range []contract.Rollup{allowed, blocked, oldBlocked} {
		if err := r.Ingest(ctx, contract.Batch{ID: []string{"allowed-old", "blocked-recent", "blocked-old"}[index], NodeID: record.NodeID, Records: []contract.Rollup{item}}, item.Bucket.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Prune(ctx, s.Now(), DefaultRetention); err != nil {
		t.Fatal(err)
	}
	items, total, err := r.List(ctx, Filter{From: s.Now().AddDate(0, 0, -120), To: s.Now()}, 1, 20)
	if err != nil || total != 1 || items[0].Allowed {
		t.Fatal("retention periods incorrect", items, total, err)
	}
	if err := submit(t, s, contract.Batch{ID: "allowed-old", NodeID: record.NodeID, Records: []contract.Rollup{allowed}}, true); err != nil {
		t.Fatal(err)
	}
	var count int
	r.Database.QueryRow("SELECT count(*) FROM access_rollups").Scan(&count)
	if count != 1 {
		t.Fatal("replay recreated expired access row")
	}
	r.Database.QueryRow("SELECT count(*) FROM traffic_daily").Scan(&count)
	if count != 1 {
		t.Fatal("daily traffic removed")
	}
	maxRecord := record
	maxRecord.Connections = math.MaxInt64
	if err := submit(t, s, contract.Batch{ID: "max", NodeID: record.NodeID, Records: []contract.Rollup{maxRecord}}, true); err != nil {
		t.Fatal(err)
	}
	if err := submit(t, s, contract.Batch{ID: "overflow", NodeID: record.NodeID, Records: []contract.Rollup{record}}, true); err == nil {
		t.Fatal("overflow accepted")
	}
	r.Database.QueryRow("SELECT count(*) FROM access_batches WHERE batch_id='overflow'").Scan(&count)
	if count != 0 {
		t.Fatal("overflow receipt committed")
	}
}

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/system"
	contract "proxycontrol/pkg/contracts/access"
	"sync"
	"testing"
	"time"
)

func TestRetentionHTTPPermissionsStrictInputAndPersistedRead(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	path := "/api/v1/system/access-retention"
	response := owner.call("GET", path, nil)
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	var settings system.RetentionSettings
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Policy != contract.DefaultRetention || settings.Revision != 0 {
		t.Fatal(settings)
	}
	input := system.RetentionInput{Policy: contract.Retention{AllowedDays: 7, BlockedDays: 30, HourlyDays: 14}}
	if response := (budgetHTTP{app: app}).call("PUT", path, input); response.Code != 401 {
		t.Fatal("anonymous settings write accepted", response.Code)
	}
	if response := (budgetHTTP{app: app, cookie: owner.cookie}).call("PUT", path, input); response.Code != 403 {
		t.Fatal("missing CSRF accepted", response.Code)
	}
	response = owner.call("PUT", path, input)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	json.Unmarshal(response.Body.Bytes(), &settings)
	if settings.Revision != 1 || settings.Policy != input.Policy || settings.UpdatedBy != "budget_owner" {
		t.Fatal(settings)
	}
	if response := owner.call("PUT", path, map[string]any{"policy": input.Policy, "expectedRevision": 1, "dailyDays": 7}); response.Code != 400 {
		t.Fatal("daily retention or unknown option accepted", response.Code)
	}
	if response := owner.call("PUT", path, system.RetentionInput{Policy: contract.Retention{AllowedDays: 0, BlockedDays: 30, HourlyDays: 14}, ExpectedRevision: 1}); response.Code != 400 {
		t.Fatal("invalid days accepted", response.Code)
	}
	if response := owner.call("PUT", path, system.RetentionInput{Policy: contract.Retention{AllowedDays: 8, BlockedDays: 30, HourlyDays: 14}}); response.Code != 409 {
		t.Fatal("stale settings overwritten", response.Code)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "retention_viewer", "password": "retention-viewer-password", "displayName": "Viewer", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	response = (budgetHTTP{app: app}).call("POST", "/api/v1/auth/login", map[string]any{"username": "retention_viewer", "password": "retention-viewer-password"})
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	var session auth.Session
	json.Unmarshal(response.Body.Bytes(), &session)
	viewer := budgetHTTP{app: app, cookie: response.Result().Cookies()[0], csrf: session.CSRFToken}
	if response := viewer.call("GET", path, nil); response.Code != 403 {
		t.Fatal("Viewer read owner setting", response.Code)
	}
	if response := viewer.call("PUT", path, input); response.Code != 403 {
		t.Fatal("Viewer changed retention", response.Code)
	}
	response = owner.call("GET", path, nil)
	var current system.RetentionSettings
	json.Unmarshal(response.Body.Bytes(), &current)
	if response.Code != 200 || current != settings {
		t.Fatal("failed writes changed saved setting", current)
	}
	var audits int
	if err := app.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='修改访问记录保留策略'").Scan(&audits); err != nil || audits != 1 {
		t.Fatal("missing real settings audit", audits, err)
	}
}

type retentionObserver struct {
	repository *system.Repository
	initial    chan struct{}
	once       sync.Once
}

func (o *retentionObserver) AccessRetentionInTx(ctx context.Context, tx *sql.Tx) (contract.Retention, error) {
	policy, err := o.repository.AccessRetentionInTx(ctx, tx)
	o.once.Do(func() { close(o.initial) })
	return policy, err
}
func TestSavedRetentionWakesCleanupAndKeepsDailyReceipts(t *testing.T) {
	ctx := context.Background()
	app := testServer(t)
	owner := budgetOwner(t, app)
	observer := &retentionObserver{repository: &system.Repository{Database: app.Database}, initial: make(chan struct{})}
	app.accessRepository.Settings = observer
	n, _, _, err := (&node.Repository{Database: app.Database}).CreateEnrollment(ctx, "Retention node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		httpListener.Close()
		t.Fatal(err)
	}
	serverCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Serve(serverCtx, httpListener, grpcListener) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(8 * time.Second):
			t.Error("retention server stop timeout")
		}
	}()
	// The first read proves the initial cleanup has begun. Subsequent Ingest
	// waits behind that transaction on the single SQLite connection.
	select {
	case <-observer.initial:
	case <-time.After(3 * time.Second):
		t.Fatal("initial cleanup did not begin")
	}
	old := contract.Rollup{Bucket: time.Now().UTC().AddDate(0, 0, -10), NodeID: n.ID, Revision: 1, InboundProtocol: "vless", Domain: "retention.example", TargetPort: 443, Network: "tcp", Action: "Direct", Allowed: true, Connections: 2, TrafficKnown: true, UploadBytes: 100, DownloadBytes: 200}
	if err := app.accessRepository.Ingest(ctx, contract.Batch{ID: "retention-allowed", NodeID: n.ID, Records: []contract.Rollup{old}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	blocked := old
	blocked.Allowed = false
	blocked.Action = "Block"
	if err := app.accessRepository.Ingest(ctx, contract.Batch{ID: "retention-blocked", NodeID: n.ID, Records: []contract.Rollup{blocked}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	response := owner.call("PUT", "/api/v1/system/access-retention", system.RetentionInput{Policy: contract.Retention{AllowedDays: 1, BlockedDays: 30, HourlyDays: 1}})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var allowed, hourly int
		if err := app.Database.QueryRow("SELECT count(*) FROM access_rollups WHERE allowed=1").Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if err := app.Database.QueryRow("SELECT count(*) FROM traffic_hourly").Scan(&hourly); err != nil {
			t.Fatal(err)
		}
		if allowed == 0 && hourly == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("saved setting did not wake automatic cleanup", allowed, hourly)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var blockedCount, daily, receipts int
	app.Database.QueryRow("SELECT count(*) FROM access_rollups WHERE allowed=0").Scan(&blockedCount)
	app.Database.QueryRow("SELECT count(*) FROM traffic_daily").Scan(&daily)
	app.Database.QueryRow("SELECT count(*) FROM access_batches").Scan(&receipts)
	if blockedCount != 1 || daily != 1 || receipts != 2 {
		t.Fatal("cleanup lost protected daily totals or receipt", blockedCount, daily, receipts)
	}
	if err := app.accessRepository.Ingest(ctx, contract.Batch{ID: "retention-allowed", NodeID: n.ID, Records: []contract.Rollup{old}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	summary, err := app.accessRepository.Summary(ctx, access.Filter{From: time.Now().AddDate(0, 0, -20), To: time.Now().Add(time.Hour)})
	if err != nil || summary.Connections != 2 || summary.Blocked != 2 {
		t.Fatal("late replay recreated expired allowance records", summary, err)
	}
}

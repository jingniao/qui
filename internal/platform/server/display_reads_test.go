package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/readtiming"
	"testing"
	"testing/fstest"
	"time"
)

// A pinned writer connection reproduces the queue seen by page requests even
// when SQLite itself is not locked. No artificial database or HTTP mock is used.
func TestDisplayReadsDoNotQueueBehindWriter(t *testing.T) {
	app := testServer(t)
	defer app.closeReadPool()
	owner := flowOwner(t, app)
	for i := 0; i < 13; i++ {
		if _, _, _, err := app.nodeRepository.CreateEnrollment(context.Background(), fmt.Sprintf("display-node-%d", i), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		response := owner.call("POST", "/api/v1/users", map[string]any{"username": fmt.Sprintf("display-user-%d", i), "displayName": "Display user", "status": "active", "allowedProtocols": []string{"socks5"}, "nodeGroupIds": []string{}})
		if response.Code != 201 {
			t.Fatalf("create display user: %d %s", response.Code, response.Body.String())
		}
	}
	held, err := app.Database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	for _, path := range []string{
		"/api/v1/users/choices", "/api/v1/users/summaries", "/api/v1/users",
		"/api/v1/nodes/choices", "/api/v1/nodes/meta", "/api/v1/nodes", "/api/v1/nodes/machine-traffic",
		"/api/v1/task-records", "/api/v1/traffic/nodes/summary", "/api/v1/traffic/nodes/monthly", "/api/v1/dashboard",
	} {
		t.Run(path, func(t *testing.T) {
			// Race instrumentation makes opening SQLite read connections costly.
			// Check writer-pool contention directly; this deadline only bounds a
			// broken request and is not a page-performance threshold.
			waiting := app.Database.Stats().WaitCount
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
			req.AddCookie(owner.cookie)
			response := httptest.NewRecorder()
			start := time.Now()
			app.HTTP.Handler.ServeHTTP(response, req)
			t.Logf("elapsed=%s status=%d", time.Since(start), response.Code)
			if got := app.Database.Stats().WaitCount; got != waiting {
				t.Fatalf("display read waited for the pinned writer: %d new waits", got-waiting)
			}
			if response.Code != http.StatusOK {
				t.Fatalf("display read blocked or failed: %s", response.Body.String())
			}
			expected := map[string]int{"/api/v1/users/choices": 4, "/api/v1/users/summaries": 4, "/api/v1/users": 4, "/api/v1/nodes/choices": 13, "/api/v1/nodes": 13}[path]
			if expected > 0 {
				var body struct {
					Total int               `json:"total"`
					Items []json.RawMessage `json:"items"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Total != expected || len(body.Items) != expected {
					t.Fatalf("missing rows: total=%d items=%d expected=%d", body.Total, len(body.Items), expected)
				}
			}
		})
	}
}

func TestDisplayReadTimingStagesAndFailure(t *testing.T) {
	app := testServer(t)
	defer app.closeReadPool()
	owner := flowOwner(t, app)
	var output bytes.Buffer
	handler := readtiming.Middleware(app.HTTP.Handler, slog.New(slog.NewJSONHandler(&output, nil)))
	req := httptest.NewRequest("GET", "/api/v1/users/choices?keyword=private-search", nil)
	req.AddCookie(owner.cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatalf("timed request: %d %s", response.Code, response.Body.String())
	}
	var entry struct {
		Stages map[string]struct {
			MS float64 `json:"ms"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"db_acquire", "db_begin", "sql", "db_end", "response_encode_write"} {
		if _, ok := entry.Stages[name]; !ok {
			t.Fatalf("missing stage %s: %s", name, output.String())
		}
	}
	if bytes.Contains(output.Bytes(), []byte("private-search")) {
		t.Fatal("query parameter leaked")
	}
	// A real missing table must remain an error, not a successful empty list or
	// a retry on the writer connection.
	if _, err := app.Database.Exec("ALTER TABLE users RENAME TO users_unavailable"); err != nil {
		t.Fatal(err)
	}
	response = owner.call("GET", "/api/v1/users/choices", nil)
	if response.Code != 500 {
		t.Fatalf("database failure hidden: %d %s", response.Code, response.Body.String())
	}
}

func TestPreparedRestoreReopensDisplayReadsAndClosesPool(t *testing.T) {
	app := testServer(t)
	owner := flowOwner(t, app)
	oldPool := app.readPool
	app.closeReadPool()
	if err := app.Database.Close(); err != nil {
		t.Fatal(err)
	}
	configuration := app.Config
	configuration.HTTPAddress = "127.0.0.1:0"
	configuration.GRPCAddress = "127.0.0.1:0"
	prepared, err := PrepareRestore(context.Background(), configuration, fstest.MapFS{"index.html": {Data: []byte("panel")}}, app.Logger)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err = oldPool.Ping(); err == nil {
		t.Fatal("old read pool still usable")
	}
	req := httptest.NewRequest("GET", "/api/v1/nodes/choices", nil)
	req.AddCookie(owner.cookie)
	response := httptest.NewRecorder()
	prepared.app.HTTP.Handler.ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatalf("restored read failed: %d %s", response.Code, response.Body.String())
	}
	reopened := prepared.app.readPool
	if _, err = reopened.Exec("DELETE FROM nodes"); err == nil {
		t.Fatal("restored reader accepted a write")
	}
	if err = prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if err = reopened.Ping(); err == nil {
		t.Fatal("prepared restore leaked its read pool")
	}
	canceledPreparation, err := PrepareRestore(context.Background(), configuration, fstest.MapFS{"index.html": {Data: []byte("panel")}}, app.Logger)
	if err != nil {
		t.Fatal(err)
	}
	defer canceledPreparation.Close()
	ownership, err := panelstore.Try(configuration.Database, configuration.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer ownership.Close()
	if err = canceledPreparation.BindOwnership(ownership); err != nil {
		t.Fatal(err)
	}
	if err = canceledPreparation.ConfigureStartupRecorder(func(context.Context) error { t.Error("canceled restore ran startup recorder"); return nil }); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	canceledPool := canceledPreparation.app.readPool
	if err = canceledPreparation.Serve(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled restore: %v", err)
	}
	if err = canceledPool.Ping(); err == nil {
		t.Fatal("canceled restore startup leaked its read pool")
	}
}

package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/db"
	"strings"
	"sync"
	"testing"
	"time"
)

// machineAudit records operation-log writes and can fail on demand.
type machineAudit struct {
	mu      sync.Mutex
	entries []string
	fail    bool
}

func (a *machineAudit) Record(_ context.Context, actor, action, target, result, _ string) error {
	return a.record(actor, action, target, result)
}

func (a *machineAudit) RecordTx(_ context.Context, _ *sql.Tx, actor, action, target, result, _ string) error {
	return a.record(actor, action, target, result)
}

func (a *machineAudit) record(actor, action, target, result string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fail {
		return errors.New("audit unavailable")
	}
	a.entries = append(a.entries, action+"|"+target+"|"+result+"|"+actor)
	return nil
}

func (a *machineAudit) recorded() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.entries...)
}

// machineSetup is the minimal first-run settings store the authentication
// service needs; this package must not import the system module.
type machineSetup struct{}

func (machineSetup) ReadSetup(context.Context) (string, string, error) {
	return "Machine", "Asia/Shanghai", nil
}

func (machineSetup) SaveSetup(context.Context, *sql.Tx, string, string) error { return nil }

type machineHTTPFixture struct {
	handler            http.Handler
	repository         *Repository
	audit              *machineAudit
	nodeID             string
	owner, ownerCSRF   string
	viewer, viewerCSRF string
}

// newMachineHTTPFixture wires the real authentication middleware in front of the
// node handler, with an Owner and a Viewer.
func newMachineHTTPFixture(t *testing.T) *machineHTTPFixture {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	audit := &machineAudit{}
	service := &auth.Service{Repository: &auth.Repository{Database: database}, Audit: audit, Settings: machineSetup{}, SessionDuration: 2 * time.Hour}
	const token, password = "machine-setup-token", "machine-password-123"
	tokenPath := filepath.Join(t.TempDir(), "setup-token")
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service.SetSetupToken(token, tokenPath)
	ownerToken, session, err := service.Setup(context.Background(), token, "owner", password, "Machine", "Asia/Shanghai", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateAdmin(context.Background(), session.CurrentUser, "viewer", "Viewer", password, "Viewer", "192.0.2.11"); err != nil {
		t.Fatal(err)
	}
	viewerToken, viewerSession, err := service.Login(context.Background(), "viewer", password, "192.0.2.12")
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{Database: database, Timezone: func(context.Context, *sql.Tx) (string, error) { return "Asia/Shanghai", nil }}
	handler := &Handler{Repository: repository, Audit: audit, Auth: &auth.Handler{Service: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	mux := http.NewServeMux()
	handler.Register(mux)
	n, _, _, err := repository.CreateEnrollment(context.Background(), "machine", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &machineHTTPFixture{handler: mux, repository: repository, audit: audit, nodeID: n.ID, owner: ownerToken, ownerCSRF: session.CSRFToken, viewer: viewerToken, viewerCSRF: viewerSession.CSRFToken}
}

func (f *machineHTTPFixture) do(t *testing.T, method, path, body, cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.10:4000"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: "proxy_session", Value: cookie})
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func TestMachineTrafficHTTPPermissionsAndAudit(t *testing.T) {
	f := newMachineHTTPFixture(t)
	settingsPath := "/api/v1/nodes/" + f.nodeID + "/machine-traffic/settings"
	summaryPath := "/api/v1/nodes/" + f.nodeID + "/machine-traffic"
	valid := `{"method":"outbound","interfaceName":"eth0","allowanceBytes":1099511627776,"resetDay":15}`
	status := func(method, path, body, cookie, csrf string) int {
		t.Helper()
		return f.do(t, method, path, body, cookie, csrf).Code
	}
	// The authentication service writes its own operation-log entries, so only
	// this feature's action is inspected.
	machineEntries := func() []string {
		entries := []string{}
		for _, entry := range f.audit.recorded() {
			if strings.HasPrefix(entry, "服务器流量设置|") {
				entries = append(entries, entry)
			}
		}
		return entries
	}
	if code := status("GET", settingsPath, "", "", ""); code != 401 {
		t.Fatalf("unauthenticated settings GET: %d", code)
	}
	if code := status("GET", "/api/v1/nodes/machine-traffic", "", "", ""); code != 401 {
		t.Fatalf("unauthenticated batch GET: %d", code)
	}
	// A Viewer reads the usage and the settings and never writes them.
	if code := status("GET", settingsPath, "", f.viewer, ""); code != 200 {
		t.Fatalf("viewer settings GET: %d", code)
	}
	if code := status("GET", summaryPath, "", f.viewer, ""); code != 200 {
		t.Fatalf("viewer summary GET: %d", code)
	}
	if code := status("PUT", settingsPath, valid, f.viewer, f.viewerCSRF); code != 403 {
		t.Fatalf("viewer settings PUT: %d", code)
	}
	// The Owner still needs the same-origin CSRF token.
	if code := status("PUT", settingsPath, valid, f.owner, ""); code != 403 {
		t.Fatalf("owner PUT without CSRF: %d", code)
	}
	for _, body := range []string{
		`{}`,
		`{"method":"maximum","interfaceName":"","allowanceBytes":null,"resetDay":1}`,
		`{"method":"sum","interfaceName":"","allowanceBytes":1.5,"resetDay":1}`,
		`{"method":"sum","interfaceName":"","allowanceBytes":0,"resetDay":1}`,
		`{"method":"sum","interfaceName":"lo","allowanceBytes":null,"resetDay":1}`,
		`{"method":"sum","interfaceName":"","allowanceBytes":null,"resetDay":32}`,
		`{"method":"sum","interfaceName":"","allowanceBytes":null,"resetDay":1,"unknown":1}`,
	} {
		if code := status("PUT", settingsPath, body, f.owner, f.ownerCSRF); code != 400 {
			t.Fatalf("invalid settings %s: %d", body, code)
		}
	}
	if rows := machineCount(t, f.repository, "SELECT count(*) FROM node_machine_traffic_settings"); rows != 0 {
		t.Fatalf("rejected requests stored %d settings rows", rows)
	}
	if entries := machineEntries(); len(entries) != 0 {
		t.Fatalf("rejected requests were audited: %v", entries)
	}
	// A valid write commits the settings and its audit entry together.
	response := f.do(t, "PUT", settingsPath, valid, f.owner, f.ownerCSRF)
	if response.Code != 200 {
		t.Fatalf("owner PUT: %d %s", response.Code, response.Body.String())
	}
	var saved MachineTrafficSettings
	read := f.do(t, "GET", settingsPath, "", f.viewer, "")
	if read.Code != 200 {
		t.Fatalf("settings GET: %d", read.Code)
	}
	if err := json.Unmarshal(read.Body.Bytes(), &saved); err != nil || saved.Method != machineMethodOutbound || saved.InterfaceName != "eth0" || saved.ResetDay != 15 || saved.AllowanceBytes == nil || *saved.AllowanceBytes != 1099511627776 {
		t.Fatalf("stored settings: %+v %v", saved, err)
	}
	if entries := machineEntries(); len(entries) != 1 || entries[0] != "服务器流量设置|"+f.nodeID+"|成功|owner" {
		t.Fatalf("audit entries: %v", entries)
	}
	// An audit failure rolls the settings back with it.
	f.audit.fail = true
	if code := status("PUT", settingsPath, `{"method":"max","interfaceName":"","allowanceBytes":null,"resetDay":1}`, f.owner, f.ownerCSRF); code != 500 {
		t.Fatalf("PUT with a failing audit: %d", code)
	}
	if kept, err := f.repository.MachineTrafficSettings(context.Background(), f.nodeID); err != nil || kept.Method != machineMethodOutbound || kept.ResetDay != 15 {
		t.Fatalf("settings survived a failed audit: %+v %v", kept, err)
	}
	f.audit.fail = false
	// Both summary endpoints answer, and a Viewer may read them.
	batch := f.do(t, "GET", "/api/v1/nodes/machine-traffic", "", f.viewer, "")
	var body struct {
		Items    []MachineTrafficSummary `json:"items"`
		Timezone string                  `json:"timezone"`
	}
	if batch.Code != 200 || json.Unmarshal(batch.Body.Bytes(), &body) != nil || len(body.Items) != 1 || body.Items[0].NodeID != f.nodeID || body.Timezone == "" {
		t.Fatalf("batch: %d %s", batch.Code, batch.Body.String())
	}
	single := f.do(t, "GET", summaryPath, "", f.viewer, "")
	var item MachineTrafficSummary
	if single.Code != 200 || json.Unmarshal(single.Body.Bytes(), &item) != nil || item.NodeID != f.nodeID || item.Status != machineStatusUnknown {
		t.Fatalf("summary: %d %s", single.Code, single.Body.String())
	}
	// A logically deleted node is not found by any of the four routes.
	if _, err := f.repository.Database.Exec("UPDATE nodes SET deleted_at=? WHERE id=?", timestamp(time.Now()), f.nodeID); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ method, path, body, csrf string }{
		{"GET", settingsPath, "", ""},
		{"GET", summaryPath, "", ""},
		{"PUT", settingsPath, valid, f.ownerCSRF},
	} {
		if code := status(request.method, request.path, request.body, f.owner, request.csrf); code != 404 {
			t.Fatalf("%s %s on a deleted node: %d", request.method, request.path, code)
		}
	}
	if batch := f.do(t, "GET", "/api/v1/nodes/machine-traffic", "", f.viewer, ""); batch.Code != 200 || json.Unmarshal(batch.Body.Bytes(), &body) != nil || len(body.Items) != 0 {
		t.Fatalf("deleted node in the batch: %d %s", batch.Code, batch.Body.String())
	}
}

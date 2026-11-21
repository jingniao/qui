package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/vault"
	contract "proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
	"time"
)

func accessRevision(t *testing.T, app *Server, nodeID string) int64 {
	t.Helper()
	ctx := context.Background()
	key, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	r := &deployment.Repository{Database: app.Database, Vault: key}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: nodeID, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: "audit-user", Enabled: true}}, Credentials: []desiredstate.Credential{{ID: "audit-credential", UserID: "audit-user", DeviceID: "audit-device", Protocol: "socks5", Username: "audit", Secret: "test-only-audit-password"}}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: 21080, Enabled: true, CredentialIDs: []string{"audit-credential"}, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}}
	revision, err := r.Create(ctx, state, "owner", "telemetry transport fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	// Synthetic telemetry transport fixture: mark an approved snapshot applied.
	// This does not claim Xray event collection or proxy traffic measurement.
	if err := r.Result(ctx, nodeID, &pb.ApplyResult{Revision: revision.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	return revision.ID
}

func telemetryRow(nodeID string, revision int64, domain string) contract.Rollup {
	return contract.Rollup{Bucket: time.Now().UTC().Truncate(time.Minute), NodeID: nodeID, Revision: revision, UserID: "audit-user", DeviceID: "audit-device", InboundProtocol: "socks5", Domain: domain, TargetPort: 443, Network: "tcp", Action: "Direct", Allowed: true, Connections: 1, TrafficKnown: true, UploadBytes: 10, DownloadBytes: 20, OutboundID: "direct"}
}

func TestAccessHTTPPaginationPrivacyAndViewerSummary(t *testing.T) {
	t.Parallel()
	app := testServer(t)
	ctx := context.Background()
	nodes := &node.Repository{Database: app.Database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "API records", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	revision := accessRevision(t, app, n.ID)
	record := telemetryRow(n.ID, revision, "private.example")
	if err := app.accessRepository.Ingest(ctx, contract.Batch{ID: "http-fixture", NodeID: n.ID, Records: []contract.Rollup{record}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		app.HTTP.Handler.ServeHTTP(response, request)
		return response
	}
	setup := call("POST", "/api/v1/auth/setup", map[string]string{"token": app.SetupToken(), "username": "owner", "password": "access-test-password-123", "platformName": "Test", "timezone": "UTC"}, nil, "")
	if setup.Code != 201 {
		t.Fatal(setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	var session auth.Session
	json.Unmarshal(setup.Body.Bytes(), &session)
	for _, path := range []string{"/api/v1/access-records", "/api/v1/access-records/blocked", "/api/v1/access-records/domains"} {
		if response := call("GET", path, nil, nil, ""); response.Code != 401 {
			t.Fatal("public access records", path, response.Code)
		}
	}
	response := call("GET", "/api/v1/access-records?page=1&pageSize=1", nil, cookie, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "private.example") || strings.Contains(response.Body.String(), "test-only-audit-password") {
		t.Fatal("record query failed or leaked credential", response.Body.String())
	}
	if response := call("GET", "/api/v1/access-records?page=0", nil, cookie, ""); response.Code != 400 {
		t.Fatal("invalid pagination accepted")
	}
	if response := call("GET", "/api/v1/access-records?domain=https://example.com/private", nil, cookie, ""); response.Code != 400 {
		t.Fatal("URL domain filter accepted")
	}
	if response := call("GET", "/api/v1/access-records/blocked", nil, cookie, ""); !strings.Contains(response.Body.String(), `"total":0`) {
		t.Fatal("blocked filter ignored")
	}
	response = call("POST", "/api/v1/admins", map[string]string{"username": "viewer", "displayName": "Viewer", "password": "access-test-password-123", "role": "Viewer"}, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	login := call("POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": "access-test-password-123"}, nil, "")
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	viewerCookie := login.Result().Cookies()[0]
	for _, path := range []string{"/api/v1/access-records", "/api/v1/access-records/blocked", "/api/v1/access-records/domains", "/api/v1/traffic/summary?userId=audit-user", "/api/v1/traffic/accounts/summary?userId=audit-user", "/api/v1/traffic/accounts/hourly?deviceId=audit-device"} {
		if response := call("GET", path, nil, viewerCookie, ""); response.Code != 403 {
			t.Fatal("Viewer read private telemetry", path, response.Code)
		}
	}
	response = call("GET", "/api/v1/traffic/summary", nil, viewerCookie, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), "private.example") {
		t.Fatal("Viewer summary failed or leaked domains", response.Body.String())
	}
	if response := call("GET", "/api/v1/traffic/accounts/summary", nil, viewerCookie, ""); response.Code != 200 || strings.Contains(response.Body.String(), "private.example") {
		t.Fatal("Viewer account aggregate failed or leaked domain")
	}
}

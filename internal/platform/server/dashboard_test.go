package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	contract "proxycontrol/pkg/contracts/access"
	"strings"
	"testing"
	"time"
)

func TestDashboardHTTPAuthenticationAndViewerDomainIsolation(t *testing.T) {
	app := testServer(t)
	ctx := context.Background()
	nodes := &node.Repository{Database: app.Database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Overview", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	record := contract.Rollup{Bucket: time.Now().UTC(), NodeID: n.ID, Revision: 1, InboundProtocol: "socks5", Domain: "private-overview.example", TargetPort: 443, Network: "tcp", Action: "Direct", Allowed: true, Connections: 1}
	if err := app.accessRepository.Ingest(ctx, contract.Batch{ID: "overview-record", NodeID: n.ID, Records: []contract.Rollup{record}}, time.Now()); err != nil {
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
	if response := call("GET", "/api/v1/dashboard", nil, nil, ""); response.Code != 401 {
		t.Fatal("unauthenticated dashboard")
	}
	setup := call("POST", "/api/v1/auth/setup", map[string]string{"token": app.SetupToken(), "username": "owner", "password": "dashboard-password-123", "platformName": "Test", "timezone": "Asia/Shanghai"}, nil, "")
	if setup.Code != 201 {
		t.Fatal(setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	var session auth.Session
	json.Unmarshal(setup.Body.Bytes(), &session)
	owner := call("GET", "/api/v1/dashboard", nil, cookie, "")
	if owner.Code != 200 || !strings.Contains(owner.Body.String(), "private-overview.example") {
		t.Fatal("owner dashboard missing real domain", owner.Body.String())
	}
	created := call("POST", "/api/v1/admins", map[string]string{"username": "viewer", "displayName": "Viewer", "password": "dashboard-password-123", "role": "Viewer"}, cookie, session.CSRFToken)
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	login := call("POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": "dashboard-password-123"}, nil, "")
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	viewer := call("GET", "/api/v1/dashboard", nil, login.Result().Cookies()[0], "")
	if viewer.Code != 200 || strings.Contains(viewer.Body.String(), "private-overview.example") || strings.Contains(viewer.Body.String(), "recentAccess") {
		t.Fatal("Viewer read private overview data", viewer.Body.String())
	}
}

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

func startTestStatistics(t *testing.T, app *Server) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); app.statistics.Run(ctx) }()
	stop := func() { cancel(); <-done }
	limit, expire := context.WithTimeout(ctx, 10*time.Second)
	defer expire()
	for {
		v, err := app.statistics.Read(limit, "")
		if err != nil || v.Error != "" {
			stop()
			t.Fatal(v, err)
		}
		if v.Status == "ready" {
			return stop
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDashboardHTTPAuthenticationAndViewerDomainIsolation(t *testing.T) {
	t.Parallel()
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
	for _, path := range []string{"/api/v1/dashboard", "/api/v1/dashboard/base", "/api/v1/dashboard/statistics", "/api/v1/statistics/nodes"} {
		if response := call("GET", path, nil, nil, ""); response.Code != 401 {
			t.Fatal("unauthenticated dashboard", path, response.Code)
		}
	}
	setup := call("POST", "/api/v1/auth/setup", map[string]string{"token": app.SetupToken(), "username": "owner", "password": "dashboard-password-123", "platformName": "Test", "timezone": "Asia/Shanghai"}, nil, "")
	if setup.Code != 201 {
		t.Fatal(setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	var session auth.Session
	json.Unmarshal(setup.Body.Bytes(), &session)
	base := call("GET", "/api/v1/dashboard/base", nil, cookie, "")
	if base.Code != 200 || !strings.Contains(base.Body.String(), "private-overview.example") || strings.Contains(base.Body.String(), `"todayUpload"`) {
		t.Fatal("owner base", base.Code, base.Body.String())
	}
	pending := call("GET", "/api/v1/dashboard/statistics", nil, cookie, "")
	if pending.Code != 200 || !strings.Contains(pending.Body.String(), `"status":"pending"`) || strings.Contains(pending.Body.String(), `"data"`) {
		t.Fatal("cold snapshot", pending.Code, pending.Body.String())
	}
	stop := startTestStatistics(t, app)
	defer stop()
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
	for _, path := range []string{"/api/v1/dashboard/base", "/api/v1/dashboard/statistics", "/api/v1/statistics/nodes?nodeId=" + n.ID} {
		response := call("GET", path, nil, login.Result().Cookies()[0], "")
		if response.Code != 200 || strings.Contains(response.Body.String(), "private-overview.example") || strings.Contains(response.Body.String(), `"recentAccess"`) || !strings.Contains(response.Body.String(), n.ID) {
			t.Fatal("viewer dashboard response", path, response.Code, response.Body.String())
		}
	}
	missing := call("GET", "/api/v1/statistics/nodes?nodeId=missing", nil, cookie, "")
	if missing.Code != 404 {
		t.Fatal(missing.Code, missing.Body.String())
	}
}

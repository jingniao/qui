package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/policy"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
	"time"
)

func TestPolicyHTTPRealSimulationAndViewerIsolation(t *testing.T) {
	app := testServer(t)
	call := func(method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
		request.RemoteAddr = "192.0.2.10:123"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		app.HTTP.Handler.ServeHTTP(response, request)
		return response
	}
	response := call("POST", "/api/v1/auth/setup", map[string]string{"token": app.SetupToken(), "username": "owner", "password": "policy-test-password-123", "platformName": "Test", "timezone": "Asia/Shanghai"}, nil, "")
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	var session auth.Session
	json.Unmarshal(response.Body.Bytes(), &session)
	nodes := &node.Repository{Database: app.Database}
	n, _, _, err := nodes.CreateEnrollment(context.Background(), "Policy node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	input := policy.Input{Name: "Netflix block", Priority: 10, Enabled: true, Scope: desiredstate.Scope{NodeIDs: []string{n.ID}}, Match: desiredstate.Match{ServiceIDs: []string{"service-netflix"}}, Action: desiredstate.Action{Type: "Block"}}
	if response := call("POST", "/api/v1/policies", input, cookie, ""); response.Code != 403 {
		t.Fatal("policy write bypassed CSRF")
	}
	response = call("POST", "/api/v1/policies", input, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var record policy.Record
	json.Unmarshal(response.Body.Bytes(), &record)
	if record.Timezone != "Asia/Shanghai" {
		t.Fatal("policy did not inherit configured zone")
	}
	response = call("POST", "/api/v1/policies/simulate", map[string]any{"nodeId": n.ID, "domain": "www.netflix.com", "port": 443, "network": "tcp"}, cookie, session.CSRFToken)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var decision struct {
		Matched  bool
		PolicyID string
		Action   desiredstate.Action
	}
	json.Unmarshal(response.Body.Bytes(), &decision)
	if !decision.Matched || decision.PolicyID != record.ID || decision.Action.Type != "Block" {
		t.Fatal("real API simulation incorrect")
	}
	response = call("POST", "/api/v1/policies/simulate", map[string]any{"nodeId": n.ID, "domain": "https://example.com/private?token=example", "port": 443, "network": "tcp"}, cookie, session.CSRFToken)
	if response.Code != 400 {
		t.Fatal("full URL simulation accepted")
	}
	response = call("POST", "/api/v1/admins", map[string]string{"username": "viewer", "displayName": "Viewer", "password": "policy-test-password-123", "role": "Viewer"}, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	response = call("POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": "policy-test-password-123"}, nil, "")
	viewerCookie := response.Result().Cookies()[0]
	var viewer auth.Session
	json.Unmarshal(response.Body.Bytes(), &viewer)
	for _, path := range []string{"/api/v1/policies", "/api/v1/service-sets"} {
		if response := call("GET", path, nil, viewerCookie, ""); response.Code != 403 {
			t.Fatal("Viewer read policy data")
		}
	}
	if response := call("POST", "/api/v1/policies/simulate", map[string]any{"nodeId": n.ID, "domain": "example.com", "port": 443, "network": "tcp"}, viewerCookie, viewer.CSRFToken); response.Code != 403 {
		t.Fatal("Viewer used private simulator")
	}
	response = call("DELETE", "/api/v1/policies/"+record.ID, nil, cookie, session.CSRFToken)
	if response.Code != 204 {
		t.Fatal(response.Body.String())
	}
	response = call("POST", "/api/v1/policies/simulate", map[string]any{"nodeId": n.ID, "domain": "www.netflix.com", "port": 443, "network": "tcp"}, cookie, session.CSRFToken)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	json.Unmarshal(response.Body.Bytes(), &decision)
	if decision.Matched || decision.Action.Type != "Direct" {
		t.Fatal("deleted policy remained in simulation")
	}
}

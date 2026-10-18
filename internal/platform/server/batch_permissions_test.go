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
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDeploymentBatchHTTPPermissionsCSRFAndSafeDTO(t *testing.T) {
	ctx := context.Background()
	app := testServer(t)
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
	response := call("POST", "/api/v1/auth/setup", map[string]string{"token": app.SetupToken(), "username": "owner", "password": "batch-test-password-123", "platformName": "Test", "timezone": "UTC"}, nil, "")
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	ownerCookie := response.Result().Cookies()[0]
	var session auth.Session
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	configurationVault, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	r := &deployment.Repository{Database: app.Database, Vault: configurationVault}
	nodes := &node.Repository{Database: app.Database}
	states := []desiredstate.State{}
	for index := 0; index < 2; index++ {
		n, _, _, err := nodes.CreateEnrollment(ctx, "Batch node", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Credentials: []desiredstate.Credential{{ID: "device", Protocol: "socks5", Username: "device", Secret: "private-batch-password"}}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: 2100 + index, Enabled: true, CredentialIDs: []string{"device"}, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}})
	}
	batch, err := r.CreateBatch(ctx, "chain", "permissions", "owner", states)
	if err != nil {
		t.Fatal(err)
	}
	readPaths := []string{"/api/v1/deployment-batches", "/api/v1/deployment-batches/" + batch.ID}
	writePaths := []string{"/api/v1/deployment-batches", "/api/v1/deployment-batches/" + batch.ID + "/deploy", "/api/v1/deployment-batches/" + batch.ID + "/rollback", "/api/v1/nodes/" + states[0].NodeID + "/rollback"}
	for _, path := range readPaths {
		if response := call("GET", path, nil, nil, ""); response.Code != 401 {
			t.Fatal("unauthenticated batch access", path, response.Code)
		}
		response := call("GET", path, nil, ownerCookie, "")
		if response.Code != 200 || strings.Contains(response.Body.String(), "private-batch-password") || strings.Contains(response.Body.String(), "encrypted_state") {
			t.Fatal("unsafe or unavailable batch DTO", response.Body.String())
		}
	}
	for _, path := range writePaths {
		if response := call("POST", path, nil, ownerCookie, ""); response.Code != 403 {
			t.Fatal("batch mutation without CSRF", path, response.Code)
		}
	}
	response = call("POST", "/api/v1/admins", map[string]string{"username": "viewer", "displayName": "Viewer", "password": "batch-test-password-123", "role": "Viewer"}, ownerCookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	response = call("POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": "batch-test-password-123"}, nil, "")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	viewerCookie := response.Result().Cookies()[0]
	var viewer auth.Session
	json.Unmarshal(response.Body.Bytes(), &viewer)
	for _, path := range readPaths {
		if response := call("GET", path, nil, viewerCookie, ""); response.Code != 403 {
			t.Fatal("Viewer read private batches", path, response.Code)
		}
	}
	for _, path := range writePaths {
		if response := call("POST", path, nil, viewerCookie, viewer.CSRFToken); response.Code != 403 {
			t.Fatal("Viewer changed deployment", path, response.Code)
		}
	}
	if response := call("POST", "/api/v1/revisions/"+strconv.FormatInt(batch.Items[1].Revision, 10)+"/deploy", nil, ownerCookie, session.CSRFToken); response.Code != 409 {
		t.Fatal("batch source bypassed publication order", response.Body.String())
	}
	response = call("POST", "/api/v1/deployment-batches/"+batch.ID+"/deploy", nil, ownerCookie, session.CSRFToken)
	if response.Code != 202 {
		t.Fatal(response.Body.String())
	}
	for _, item := range batch.Items {
		if err := r.Result(ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	response = call("POST", "/api/v1/nodes/"+states[0].NodeID+"/rollback", map[string]int64{"revision": batch.Items[1].Revision}, ownerCookie, session.CSRFToken)
	if response.Code != 409 {
		t.Fatal("node accepted another node's revision")
	}
	response = call("POST", "/api/v1/deployment-batches/"+batch.ID+"/rollback", nil, ownerCookie, session.CSRFToken)
	if response.Code != 202 {
		t.Fatal(response.Body.String())
	}
}

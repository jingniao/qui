package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRevisionDiffHTTPPermissionsValidationAndRealImmutableChanges(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	nodes := &node.Repository{Database: app.Database}
	n, _, _, err := nodes.CreateEnrollment(context.Background(), "Compare node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true},
		Credentials: []desiredstate.Credential{{ID: "credential", Protocol: "http", Username: "private-http-account", Secret: "private-http-first-password"}},
		Inbounds:    []desiredstate.Inbound{{ID: "entry", Protocol: "http", Listen: "127.0.0.1", Port: 1080, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"credential"}}},
		Outbounds:   []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
	create := func() deployment.Revision {
		t.Helper()
		response := owner.call("POST", "/api/v1/revisions", map[string]any{"summary": "Review changes", "state": state})
		var revision deployment.Revision
		if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &revision) != nil {
			t.Fatal(response.Code, response.Body.String())
		}
		return revision
	}
	first := create()
	state.Inbounds[0].Port = 1081
	state.Credentials[0].Secret = "private-http-second-password"
	second := create()
	path := "/api/v1/revisions/" + strconv.FormatInt(second.ID, 10) + "/diff"
	if response := (budgetHTTP{app: app}).call("GET", path, nil); response.Code != 401 {
		t.Fatal("anonymous comparison accepted", response.Code)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "diff_viewer", "password": "diff-viewer-password", "displayName": "Viewer", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	login := (budgetHTTP{app: app}).call("POST", "/api/v1/auth/login", map[string]any{"username": "diff_viewer", "password": "diff-viewer-password"})
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	var session auth.Session
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	viewer := budgetHTTP{app: app, cookie: login.Result().Cookies()[0], csrf: session.CSRFToken}
	if response := viewer.call("GET", path, nil); response.Code != 403 {
		t.Fatal("Viewer read configuration diff", response.Code)
	}
	for _, query := range []string{"?baseRevision=-1", "?baseRevision=bad", "?baseRevision=", "?baseRevision=1&baseRevision=2", "?baseRevision=9223372036854775808"} {
		if response := owner.call("GET", path+query, nil); response.Code != 400 {
			t.Fatal("invalid comparison accepted", query, response.Code)
		}
	}
	if response := owner.call("GET", "/api/v1/revisions/bad/diff", nil); response.Code != 400 {
		t.Fatal(response.Code)
	}
	if response := owner.call("GET", path+"?baseRevision=999999", nil); response.Code != 404 {
		t.Fatal("missing base accepted", response.Code)
	}
	if response := owner.call("GET", "/api/v1/revisions/999999/diff", nil); response.Code != 404 {
		t.Fatal("missing target accepted", response.Code)
	}
	otherNode, _, _, err := nodes.CreateEnrollment(context.Background(), "Other comparison node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	state.NodeID = otherNode.ID
	other := create()
	if response := owner.call("GET", path+"?baseRevision="+strconv.FormatInt(other.ID, 10), nil); response.Code != 400 {
		t.Fatal("cross-node comparison accepted", response.Code)
	}
	if _, err := app.Database.Exec("INSERT INTO node_runtime_state(node_id,revision) VALUES(?,?)", n.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	// Exercise the live HTTP transport, cookie authentication and no-store
	// response as well as direct handler validation above.
	server := httptest.NewServer(app.HTTP.Handler)
	defer server.Close()
	request, err := http.NewRequest("GET", server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(owner.cookie)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var diff deployment.RevisionDiff
	if response.StatusCode != 200 || json.Unmarshal(data, &diff) != nil || diff.BaseRevision != first.ID || diff.Revision != second.ID || len(diff.Changes) != 2 {
		t.Fatal("wrong live HTTP comparison", response.StatusCode, string(data))
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("configuration comparison may be cached")
	}
	for _, marker := range []string{"private-http-first-password", "private-http-second-password", "private-http-account", "encrypted_state", "desired_state"} {
		if strings.Contains(string(data), marker) {
			t.Fatal("comparison disclosed snapshot or credentials")
		}
	}
	if response := owner.call("GET", path+"?baseRevision=0", nil); response.Code != 200 {
		t.Fatal("empty comparison rejected", response.Code)
	}
	if response := owner.call("GET", path+"?baseRevision="+strconv.FormatInt(second.ID, 10), nil); response.Code != 200 || !strings.Contains(response.Body.String(), `"changes":[]`) {
		t.Fatal("identical version had changes", response.Code)
	}
}

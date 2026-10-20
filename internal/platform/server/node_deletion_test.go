package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
)

// nodeDeletionFixture creates one enrolled node directly, because the enrollment
// handshake is covered elsewhere and only the deletion is under test here.
func enrolledTestNode(t *testing.T, app *Server, name string) string {
	t.Helper()
	ctx := context.Background()
	repository := &node.Repository{Database: app.Database}
	n, _, _, err := repository.CreateEnrollment(ctx, name, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Database.ExecContext(ctx, "INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES(?,'pem',?,'x')", n.ID, "fingerprint-"+n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Database.ExecContext(ctx, "UPDATE nodes SET status='offline' WHERE id=?", n.ID); err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func TestNodeDeletionPermissionsPreviewAndBlockers(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	nodeID := enrolledTestNode(t, app, "Delete me")
	if _, err := app.Database.ExecContext(ctx, "INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('i1','In',?,'socks5','127.0.0.1',1080,1,1,0,'direct','[]',x'00','x')", nodeID); err != nil {
		t.Fatal(err)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "node_viewer", "password": "viewer-test-password", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	login := (budgetHTTP{app: app}).call("POST", "/api/v1/auth/login", map[string]any{"username": "node_viewer", "password": "viewer-test-password"})
	var session auth.Session
	if json.Unmarshal(login.Body.Bytes(), &session) != nil || login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	viewer := budgetHTTP{app: app, cookie: login.Result().Cookies()[0], csrf: session.CSRFToken}
	for _, item := range []struct {
		client budgetHTTP
		method string
		path   string
		status int
	}{
		{budgetHTTP{app: app}, "DELETE", "/api/v1/nodes/" + nodeID, 401},
		{budgetHTTP{app: app}, "GET", "/api/v1/nodes/" + nodeID + "/deletion-preview", 401},
		{viewer, "DELETE", "/api/v1/nodes/" + nodeID, 403},
		{viewer, "GET", "/api/v1/nodes/" + nodeID + "/deletion-preview", 403},
	} {
		if response := item.client.call(item.method, item.path, nil); response.Code != item.status {
			t.Fatalf("%s %s: %d %s", item.method, item.path, response.Code, response.Body.String())
		}
	}
	preview := owner.call("GET", "/api/v1/nodes/"+nodeID+"/deletion-preview", nil)
	var view struct {
		Mode     string         `json:"mode"`
		Blockers []node.Blocker `json:"blockers"`
	}
	if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &view) != nil || view.Mode != "delete" || len(view.Blockers) != 1 || view.Blockers[0].Kind != "inbound" || view.Blockers[0].Name != "In" {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	blocked := owner.call("DELETE", "/api/v1/nodes/"+nodeID, nil)
	var failure struct {
		Error struct {
			Code     string         `json:"code"`
			Message  string         `json:"message"`
			Blockers []node.Blocker `json:"blockers"`
		} `json:"error"`
	}
	if blocked.Code != 409 || json.Unmarshal(blocked.Body.Bytes(), &failure) != nil || failure.Error.Code != "node_delete_blocked" || len(failure.Error.Blockers) != 1 || failure.Error.Blockers[0].Kind != "inbound" || failure.Error.Message == "" {
		t.Fatalf("blocked delete: %d %s", blocked.Code, blocked.Body.String())
	}
	var deleted int
	if err := app.Database.QueryRowContext(ctx, "SELECT count(*) FROM nodes WHERE id=? AND deleted_at IS NOT NULL", nodeID).Scan(&deleted); err != nil || deleted != 0 {
		t.Fatalf("blocked delete changed the node: %d %v", deleted, err)
	}
}

func TestNodeDeletionLogicalDeleteAndFollowUpRefusals(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	nodeID := enrolledTestNode(t, app, "Gone")
	preview := owner.call("GET", "/api/v1/nodes/"+nodeID+"/deletion-preview", nil)
	var view struct {
		Mode     string         `json:"mode"`
		Blockers []node.Blocker `json:"blockers"`
	}
	if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &view) != nil || view.Mode != "delete" || len(view.Blockers) != 0 {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	for attempt := 0; attempt < 2; attempt++ {
		if response := owner.call("DELETE", "/api/v1/nodes/"+nodeID, nil); response.Code != 204 {
			t.Fatalf("delete %d: %d %s", attempt, response.Code, response.Body.String())
		}
	}
	var operations int
	if err := app.Database.QueryRowContext(ctx, "SELECT count(*) FROM admin_operations WHERE action='删除节点'").Scan(&operations); err != nil || operations != 1 {
		t.Fatalf("audit rows: %d %v", operations, err)
	}
	if response := owner.call("PUT", "/api/v1/nodes/"+nodeID, map[string]any{"name": "Renamed", "enabled": true}); response.Code != 409 || !strings.Contains(response.Body.String(), "node_deleted") {
		t.Fatalf("deleted node accepted settings: %d %s", response.Code, response.Body.String())
	}
	if response := owner.call("POST", "/api/v1/outbounds", map[string]any{"name": "Out", "nodeId": nodeID, "protocol": "socks5"}); response.Code != 400 {
		t.Fatalf("deleted node accepted an outbound: %d %s", response.Code, response.Body.String())
	}
	if response := owner.call("POST", "/api/v1/node-groups", map[string]any{"name": "Group", "purpose": "subscription", "enabled": true, "nodeIds": []string{nodeID}}); response.Code != 400 {
		t.Fatalf("deleted node joined a group: %d %s", response.Code, response.Body.String())
	}
	var list struct {
		Items []node.Node `json:"items"`
		Total int         `json:"total"`
	}
	if response := owner.call("GET", "/api/v1/nodes", nil); response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil || list.Total != 0 {
		t.Fatalf("deleted node is still listed: %d %s", response.Code, response.Body.String())
	}
	if response := owner.call("GET", "/api/v1/nodes?deleted=1", nil); response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil || list.Total != 1 || len(list.Items) != 1 || list.Items[0].DeletedAt == "" {
		t.Fatalf("deleted node missing from the deleted list: %d %s", response.Code, response.Body.String())
	}
	if response := owner.call("GET", "/api/v1/nodes/"+nodeID+"/deletion-preview", nil); response.Code != 200 || !strings.Contains(response.Body.String(), `"mode":"deleted"`) {
		t.Fatalf("deleted preview: %d %s", response.Code, response.Body.String())
	}
}

// A pending node that never enrolled keeps the cancel path: the row disappears.
func TestNodeDeletionCancelsPendingNode(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	repository := &node.Repository{Database: app.Database}
	n, token, _, err := repository.CreateEnrollment(context.Background(), "Pending", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if response := owner.call("GET", "/api/v1/nodes/"+n.ID+"/deletion-preview", nil); response.Code != 200 || !strings.Contains(response.Body.String(), `"mode":"cancel"`) {
		t.Fatalf("pending preview: %d %s", response.Code, response.Body.String())
	}
	if response := owner.call("DELETE", "/api/v1/nodes/"+n.ID, nil); response.Code != 204 {
		t.Fatalf("cancel: %d %s", response.Code, response.Body.String())
	}
	if _, err := repository.Get(context.Background(), n.ID); err == nil {
		t.Fatal("cancelled node is still listed")
	}
	if _, err := repository.EnrollmentNode(context.Background(), node.Hash(token)); err == nil {
		t.Fatal("cancelled node still enrolls")
	}
	var operations int
	if err := app.Database.QueryRowContext(context.Background(), "SELECT count(*) FROM admin_operations WHERE action='取消节点登记'").Scan(&operations); err != nil || operations != 1 {
		t.Fatalf("cancel audit rows: %d %v", operations, err)
	}
}

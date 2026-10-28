package deployment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"strings"
	"testing"
	"time"
)

// publishAudit records the audit rows a publication writes, so a test can check
// that one-step publishing still leaves exactly one trail.
type publishAudit struct {
	actions []string
	targets []string
}

func (a *publishAudit) Record(_ context.Context, _ string, action, target, _ string, _ string) error {
	a.actions = append(a.actions, action)
	a.targets = append(a.targets, target)
	return nil
}

// publishSettings answers the two settings reads auth.Service makes while it
// resolves a session.
type publishSettings struct{}

func (publishSettings) SaveSetup(context.Context, *sql.Tx, string, string) error { return nil }
func (publishSettings) ReadSetup(context.Context) (string, string, error) {
	return "Test Control", "UTC", nil
}

// publishPanel is the smallest panel the one-step publication needs: two
// enrolled nodes, the proxy service that renders their configuration, an audit
// trail and the real authentication middleware, which the tests drive with a
// session cookie so the Owner-only route is exercised as it is served.
type publishPanel struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
	nodes    *node.Repository
	handler  *Handler
	audit    *publishAudit
	vault    *vault.Vault
	nodeA    string
	nodeB    string
}

func newPublishPanel(t *testing.T) *publishPanel {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	first, _, _, err := nodes.CreateEnrollment(ctx, "Node A", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := nodes.CreateEnrollment(ctx, "Node B", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	audit := &publishAudit{}
	handler := &Handler{
		Repository: &Repository{Database: database, Vault: key},
		Nodes:      nodes,
		Auth: &auth.Handler{
			Service: &auth.Service{Repository: &auth.Repository{Database: database}, Settings: publishSettings{}, SessionDuration: time.Hour},
			Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		Audit: audit,
		Users: stubUserAccess{},
		Proxy: &proxy.Service{Repository: &proxy.Repository{Database: database, Vault: key}, Nodes: nodes},
	}
	return &publishPanel{t: t, ctx: ctx, database: database, nodes: nodes, handler: handler, audit: audit, vault: key, nodeA: first.ID, nodeB: second.ID}
}

// session inserts one administrator with a live session and returns the cookie
// a browser would send together with the CSRF token derived from it.
func (p *publishPanel) session(role string) (*http.Cookie, string) {
	p.t.Helper()
	digest := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	token := "session-" + strings.ToLower(role)
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	if _, err := p.database.ExecContext(p.ctx, "INSERT INTO admins(id,username,display_name,role,status,password_hash,created_at) VALUES(?,?,?,?,'active','unused',?)", role, role, role, role, stamp); err != nil {
		p.t.Fatal(err)
	}
	expires := time.Now().UTC().Add(time.Hour).Format("2006-01-02T15:04:05.000000000Z")
	if _, err := p.database.ExecContext(p.ctx, "INSERT INTO sessions(token_hash,admin_id,expires_at,created_at) VALUES(?,?,?,?)", digest(token), role, expires, stamp); err != nil {
		p.t.Fatal(err)
	}
	return &http.Cookie{Name: "proxy_session", Value: token}, digest("csrf:" + token)
}

func (p *publishPanel) publish(nodeID, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	p.t.Helper()
	mux := http.NewServeMux()
	p.handler.Register(mux)
	request := httptest.NewRequest("POST", "/api/v1/nodes/"+nodeID+"/publish", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

// relayOutbound keeps an enabled relay outbound on a node. The settings blob is
// sealed by the same vault the proxy repository reads it with, so the snapshot
// renders the outbound the way the Owner's outbound page would have saved it.
func (p *publishPanel) relayOutbound(nodeID, targetNodeID string) {
	p.t.Helper()
	sealed, err := p.vault.Seal([]byte("{}"))
	if err != nil {
		p.t.Fatal(err)
	}
	if _, err := p.database.ExecContext(p.ctx, "INSERT INTO outbounds(id,name,node_id,protocol,address,port,enabled,target_node_id,credential_id,encrypted_settings,support_tcp,support_udp,failure_policy,backup_outbound_id,created_at) VALUES('relay','Relay',?,'socks5','192.0.2.20',1080,1,?,'',?,1,1,'Block','','x')", nodeID, targetNodeID, sealed); err != nil {
		p.t.Fatal(err)
	}
}

func (p *publishPanel) revisionStatus(id int64) string {
	p.t.Helper()
	var status string
	if err := p.database.QueryRowContext(p.ctx, "SELECT status FROM config_revisions WHERE id=?", id).Scan(&status); err != nil {
		p.t.Fatal(err)
	}
	return status
}

// The one-step publication replaces the two calls the retired revision page
// made: it renders the node's draft and queues it on that node in one request.
func TestPublishNodeCreatesAndDeploysRevision(t *testing.T) {
	p := newPublishPanel(t)
	cookie, csrf := p.session("Owner")
	response := p.publish(p.nodeA, `{"summary":"Publish node"}`, cookie, csrf)
	if response.Code != 202 {
		t.Fatalf("publish: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Revision int64  `json:"revision"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Revision < 1 || body.Status != "pending" {
		t.Fatalf("publish answered %+v", body)
	}
	if status := p.revisionStatus(body.Revision); status != "pending" {
		t.Fatalf("the revision is %s, want pending", status)
	}
	target, err := p.handler.Repository.TargetStatus(p.ctx, p.nodeA, body.Revision)
	if err != nil || target != "pending" {
		t.Fatalf("the node target is %q (%v), want pending", target, err)
	}
	items, total, err := p.handler.Repository.ListForNode(p.ctx, p.nodeA, 1, 20)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != body.Revision {
		t.Fatalf("the node's history holds %d of %d revisions (%v): %+v", len(items), total, err, items)
	}
	if len(p.audit.actions) != 1 || p.audit.actions[0] != "发布节点配置" || p.audit.targets[0] != p.nodeA {
		t.Fatalf("publication audited %v %v", p.audit.actions, p.audit.targets)
	}
}

// A relay outbound whose target was never published keeps the deploy away from
// the node, but the draft stays in the node's history and the refusal names it,
// so the Owner retries that revision instead of saving the data again.
func TestPublishNodeKeepsDraftWhenDependencyNotReady(t *testing.T) {
	p := newPublishPanel(t)
	p.relayOutbound(p.nodeA, p.nodeB)
	cookie, csrf := p.session("Owner")
	response := p.publish(p.nodeA, `{"summary":"Publish relay"}`, cookie, csrf)
	if response.Code != 409 {
		t.Fatalf("publish with an unpublished relay target: %d %s", response.Code, response.Body.String())
	}
	var failure struct {
		Error struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			Revision int64  `json:"revision"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Error.Code != "dependency_not_ready" || failure.Error.Revision < 1 {
		t.Fatalf("refused publication reported %+v", failure.Error)
	}
	if status := p.revisionStatus(failure.Error.Revision); status != "draft" {
		t.Fatalf("the refused publication left the revision %s, want draft", status)
	}
	if target, err := p.handler.Repository.TargetStatus(p.ctx, p.nodeA, failure.Error.Revision); err != nil || target != "draft" {
		t.Fatalf("the refused publication touched the node target: %q %v", target, err)
	}
	if len(p.audit.actions) != 0 {
		t.Fatalf("a refused publication was audited as success: %v", p.audit.actions)
	}
}

func TestPublishNodeRequiresOwner(t *testing.T) {
	p := newPublishPanel(t)
	cookie, csrf := p.session("Viewer")
	response := p.publish(p.nodeA, `{"summary":"Publish node"}`, cookie, csrf)
	if response.Code != 403 {
		t.Fatalf("viewer published a node: %d %s", response.Code, response.Body.String())
	}
	if _, total, err := p.handler.Repository.ListForNode(p.ctx, p.nodeA, 1, 20); err != nil || total != 0 {
		t.Fatalf("the refused publication created revisions: %d %v", total, err)
	}
}

// A node the proxy renderer cannot render - unknown, or disabled - is a bad
// snapshot and fails before a revision exists.
func TestPublishNodeRejectsUnrenderableSnapshot(t *testing.T) {
	p := newPublishPanel(t)
	cookie, csrf := p.session("Owner")
	if response := p.publish("missing-node", `{"summary":"Publish node"}`, cookie, csrf); response.Code != 400 {
		t.Fatalf("unknown node: %d %s", response.Code, response.Body.String())
	}
	if err := p.nodes.UpdateSettings(p.ctx, p.nodeA, node.SettingsInput{Name: "Node A", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if response := p.publish(p.nodeA, `{"summary":"Publish node"}`, cookie, csrf); response.Code != 400 {
		t.Fatalf("disabled node: %d %s", response.Code, response.Body.String())
	}
	if _, total, err := p.handler.Repository.List(p.ctx, 1, 20); err != nil || total != 0 {
		t.Fatalf("a rejected snapshot created revisions: %d %v", total, err)
	}
}

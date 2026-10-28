package proxy

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
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"strconv"
	"strings"
	"testing"
	"time"
)

// canvasAudit records the trail the canvas endpoints write, so a test can check
// the three audit actions without a running system module.
type canvasAudit struct {
	actions []string
	targets []string
}

func (a *canvasAudit) Record(_ context.Context, _ string, action, target, _ string, _ string) error {
	a.actions = append(a.actions, action)
	a.targets = append(a.targets, target)
	return nil
}

// canvasSettings answers the two settings reads auth.Service makes while it
// resolves a session.
type canvasSettings struct{}

func (canvasSettings) SaveSetup(context.Context, *sql.Tx, string, string) error { return nil }
func (canvasSettings) ReadSetup(context.Context) (string, string, error) {
	return "Test Control", "UTC", nil
}

// canvasPanel is the smallest panel the canvas endpoints need: two enrolled
// nodes, one inbound on the second one, the proxy service, an audit trail and
// the real authentication middleware. Requests go through a session cookie, so
// the Owner-only routes are exercised exactly as the panel serves them.
type canvasPanel struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
	service  *Service
	mux      http.Handler
	audit    *canvasAudit
	nodeA    string
	nodeB    string
	inbound  Inbound
}

func newCanvasPanel(t *testing.T) *canvasPanel {
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
	nodes.SetPublicIP(ctx, first.ID, "192.0.2.10")
	nodes.SetPublicIP(ctx, second.ID, "192.0.2.11")
	service := &Service{Repository: &Repository{Database: database, Vault: key}, Nodes: nodes}
	inbound, err := service.SaveInbound(ctx, "", InboundInput{Name: "Relay entry", NodeID: second.ID, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	audit := &canvasAudit{}
	handler := &Handler{
		Service: service,
		Auth: &auth.Handler{
			Service: &auth.Service{Repository: &auth.Repository{Database: database}, Settings: canvasSettings{}, SessionDuration: time.Hour},
			Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		Audit: audit,
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	return &canvasPanel{t: t, ctx: ctx, database: database, service: service, mux: mux, audit: audit, nodeA: first.ID, nodeB: second.ID, inbound: inbound}
}

// session inserts one administrator with a live session and returns the cookie
// a browser would send together with the CSRF token derived from it.
func (p *canvasPanel) session(role string) (*http.Cookie, string) {
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

func (p *canvasPanel) call(method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	p.t.Helper()
	var data []byte
	if text, ok := body.(string); ok {
		data = []byte(text)
	} else if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			p.t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	recorder := httptest.NewRecorder()
	p.mux.ServeHTTP(recorder, request)
	return recorder
}

// layout is a valid A to B drawing: a server instance and an inbound instance
// joined by a TLS tunnel. Each test breaks one part of it at a time.
func (p *canvasPanel) layout() map[string]any {
	return map[string]any{
		"instances": []any{
			map[string]any{"instanceId": "inst-a", "ref": "server:" + p.nodeA, "x": 120, "y": 200, "role": "entry", "listenPort": 10081},
			map[string]any{"instanceId": "inst-b", "ref": "inbound:" + p.inbound.ID, "x": 520, "y": 200, "role": "exit", "targetPort": 443},
		},
		"connections": []any{
			map[string]any{"id": "conn-ab", "source": "inst-a", "target": "inst-b", "type": "tunnel", "tunnelProtocol": "tls", "tunnelPort": 10091, "supportTcp": true, "supportUdp": true},
		},
	}
}

func (p *canvasPanel) create(name string, layout map[string]any, cookie *http.Cookie, csrf string) CanvasScheme {
	p.t.Helper()
	response := p.call("POST", "/api/v1/canvas-schemes", map[string]any{"name": name, "execution": "server", "layout": layout}, cookie, csrf)
	if response.Code != 201 {
		p.t.Fatalf("create scheme: %d %s", response.Code, response.Body.String())
	}
	var scheme CanvasScheme
	if err := json.Unmarshal(response.Body.Bytes(), &scheme); err != nil {
		p.t.Fatal(err)
	}
	return scheme
}

func decodeCanvasScheme(t *testing.T, response *httptest.ResponseRecorder) CanvasScheme {
	t.Helper()
	var scheme CanvasScheme
	if err := json.Unmarshal(response.Body.Bytes(), &scheme); err != nil {
		t.Fatalf("%v: %s", err, response.Body.String())
	}
	return scheme
}

// body builds a create or update request around one layout.
func (p *canvasPanel) body(name string, layout map[string]any) map[string]any {
	return map[string]any{"name": name, "execution": "server", "layout": layout}
}

func (p *canvasPanel) schemeCount() int {
	p.t.Helper()
	var count int
	if err := p.database.QueryRowContext(p.ctx, "SELECT count(*) FROM canvas_schemes").Scan(&count); err != nil {
		p.t.Fatal(err)
	}
	return count
}

// TestCanvasSchemeRoundTripAndAudit walks create, list, read, update and delete
// through the served routes and checks the three audit actions they leave.
func TestCanvasSchemeRoundTripAndAudit(t *testing.T) {
	p := newCanvasPanel(t)
	cookie, csrf := p.session("Owner")
	created := p.create("亚太日本中转", p.layout(), cookie, csrf)
	if created.ID == "" || created.Name != "亚太日本中转" || created.Execution != "server" || created.UpdatedAt == "" || created.PublishedAt != "" || created.PublishedBatchID != "" {
		t.Fatalf("created scheme: %+v", created)
	}
	if len(created.Layout.Instances) != 2 || len(created.Layout.Connections) != 1 {
		t.Fatalf("layout did not round-trip: %+v", created.Layout)
	}
	second := created.Layout.Instances[1]
	if second.Ref != "inbound:"+p.inbound.ID || second.TargetPort == nil || *second.TargetPort != 443 {
		t.Fatalf("layout lost a reference or a port: %+v", second)
	}
	tunnel := created.Layout.Connections[0]
	if tunnel.TunnelProtocol != "tls" || tunnel.TunnelPort == nil || *tunnel.TunnelPort != 10091 || !tunnel.SupportTCP {
		t.Fatalf("layout lost the tunnel: %+v", tunnel)
	}

	list := p.call("GET", "/api/v1/canvas-schemes", nil, cookie, "")
	if list.Code != 200 {
		t.Fatalf("list schemes: %d %s", list.Code, list.Body.String())
	}
	var page struct {
		Items []CanvasScheme `json:"items"`
		Total int            `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("list did not return the stored scheme: %+v", page)
	}

	read := p.call("GET", "/api/v1/canvas-schemes/"+created.ID, nil, cookie, "")
	if read.Code != 200 || decodeCanvasScheme(t, read).Name != created.Name {
		t.Fatalf("read scheme: %d %s", read.Code, read.Body.String())
	}

	update := p.body("改名后的方案", p.layout())
	update["updatedAt"] = created.UpdatedAt
	updated := p.call("PUT", "/api/v1/canvas-schemes/"+created.ID, update, cookie, csrf)
	if updated.Code != 200 {
		t.Fatalf("update scheme: %d %s", updated.Code, updated.Body.String())
	}
	changed := decodeCanvasScheme(t, updated)
	if changed.Name != "改名后的方案" || changed.UpdatedAt == created.UpdatedAt || changed.CreatedAt != created.CreatedAt {
		t.Fatalf("update did not move the version: %+v", changed)
	}
	if changed.PublishedAt != "" || changed.PublishedBatchID != "" {
		t.Fatalf("a draft update published the scheme: %+v", changed)
	}

	deleted := p.call("DELETE", "/api/v1/canvas-schemes/"+created.ID, nil, cookie, csrf)
	if deleted.Code != 204 {
		t.Fatalf("delete scheme: %d %s", deleted.Code, deleted.Body.String())
	}
	if missing := p.call("GET", "/api/v1/canvas-schemes/"+created.ID, nil, cookie, ""); missing.Code != 404 {
		t.Fatalf("deleted scheme still readable: %d %s", missing.Code, missing.Body.String())
	}
	if p.schemeCount() != 0 {
		t.Fatal("deleted scheme left a row")
	}
	want := []string{"创建中转方案", "修改中转方案", "删除中转方案"}
	if len(p.audit.actions) != len(want) {
		t.Fatalf("audit actions: %v", p.audit.actions)
	}
	for index, action := range want {
		if p.audit.actions[index] != action {
			t.Fatalf("audit actions: %v", p.audit.actions)
		}
	}
}

// TestCanvasSchemeStaleUpdateReturnsConflict holds the version token a second
// canvas would still have after the first one saved.
func TestCanvasSchemeStaleUpdateReturnsConflict(t *testing.T) {
	p := newCanvasPanel(t)
	cookie, csrf := p.session("Owner")
	created := p.create("并发方案", p.layout(), cookie, csrf)
	first := p.body("第一次保存", p.layout())
	first["updatedAt"] = created.UpdatedAt
	if response := p.call("PUT", "/api/v1/canvas-schemes/"+created.ID, first, cookie, csrf); response.Code != 200 {
		t.Fatalf("first update: %d %s", response.Code, response.Body.String())
	}
	stale := p.body("过期画布", p.layout())
	stale["updatedAt"] = created.UpdatedAt
	response := p.call("PUT", "/api/v1/canvas-schemes/"+created.ID, stale, cookie, csrf)
	if response.Code != 409 {
		t.Fatalf("stale update: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "stale_scheme" || body.Error.Message != "方案已在其他地方修改，请刷新" {
		t.Fatalf("stale error body: %+v", body.Error)
	}
	// An update without a token cannot prove which version it edited.
	missing := p.body("无令牌", p.layout())
	if response := p.call("PUT", "/api/v1/canvas-schemes/"+created.ID, missing, cookie, csrf); response.Code != 409 {
		t.Fatalf("update without a token: %d %s", response.Code, response.Body.String())
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, created.ID)
	if err != nil || stored.Name != "第一次保存" {
		t.Fatalf("refused update changed the scheme: %+v %v", stored, err)
	}
}

// TestCanvasSchemeViewerIsRefused keeps the drawing Owner-only.
func TestCanvasSchemeViewerIsRefused(t *testing.T) {
	p := newCanvasPanel(t)
	cookie, csrf := p.session("Viewer")
	if response := p.call("GET", "/api/v1/canvas-schemes", nil, cookie, ""); response.Code != 403 {
		t.Fatalf("viewer list: %d %s", response.Code, response.Body.String())
	}
	if response := p.call("POST", "/api/v1/canvas-schemes", p.body("查看者方案", p.layout()), cookie, csrf); response.Code != 403 {
		t.Fatalf("viewer create: %d %s", response.Code, response.Body.String())
	}
	if p.schemeCount() != 0 {
		t.Fatal("refused request stored a scheme")
	}
}

// TestCanvasSchemeRefusesInvalidDrawings covers every rule a layout must satisfy
// before it is stored.
func TestCanvasSchemeRefusesInvalidDrawings(t *testing.T) {
	instance := func(layout map[string]any) map[string]any {
		return layout["instances"].([]any)[0].(map[string]any)
	}
	connection := func(layout map[string]any) map[string]any {
		return layout["connections"].([]any)[0].(map[string]any)
	}
	cases := []struct {
		name  string
		build func(*canvasPanel) any
	}{
		{"unknown reference kind", func(p *canvasPanel) any {
			layout := p.layout()
			instance(layout)["ref"] = "cluster:" + p.nodeA
			return p.body("坏引用", layout)
		}},
		{"missing node", func(p *canvasPanel) any {
			layout := p.layout()
			instance(layout)["ref"] = "server:missing"
			return p.body("节点不存在", layout)
		}},
		{"deleted node", func(p *canvasPanel) any {
			if _, err := p.database.ExecContext(p.ctx, "UPDATE nodes SET deleted_at=? WHERE id=?", "2026-10-27T00:00:00.000000000Z", p.nodeA); err != nil {
				p.t.Fatal(err)
			}
			return p.body("已删除节点", p.layout())
		}},
		{"missing inbound", func(p *canvasPanel) any {
			layout := p.layout()
			layout["instances"].([]any)[1].(map[string]any)["ref"] = "inbound:missing"
			return p.body("入站不存在", layout)
		}},
		{"self loop", func(p *canvasPanel) any {
			layout := p.layout()
			connection(layout)["target"] = "inst-a"
			return p.body("自环", layout)
		}},
		{"duplicate edge", func(p *canvasPanel) any {
			layout := p.layout()
			duplicate := map[string]any{"id": "conn-ab2", "source": "inst-a", "target": "inst-b", "type": "proxy_chain", "supportTcp": true, "supportUdp": true}
			layout["connections"] = append(layout["connections"].([]any), duplicate)
			return p.body("重复连线", layout)
		}},
		{"connection to an unknown instance", func(p *canvasPanel) any {
			layout := p.layout()
			connection(layout)["target"] = "inst-z"
			return p.body("断线", layout)
		}},
		{"listen port above range", func(p *canvasPanel) any {
			layout := p.layout()
			instance(layout)["listenPort"] = 70000
			return p.body("坏端口", layout)
		}},
		{"listen port zero", func(p *canvasPanel) any {
			layout := p.layout()
			instance(layout)["listenPort"] = 0
			return p.body("零端口", layout)
		}},
		{"tunnel port above range", func(p *canvasPanel) any {
			layout := p.layout()
			connection(layout)["tunnelPort"] = 70000
			return p.body("坏隧道端口", layout)
		}},
		{"too many instances", func(p *canvasPanel) any {
			instances := []any{}
			for index := 0; index <= canvasMaxInstances; index++ {
				instances = append(instances, map[string]any{"instanceId": "inst-" + strconv.Itoa(index), "ref": "import:manual-" + strconv.Itoa(index), "x": 10, "y": 10, "role": "relay"})
			}
			return p.body("过多实例", map[string]any{"instances": instances, "connections": []any{}})
		}},
		{"unknown layout field", func(p *canvasPanel) any {
			layout := p.layout()
			instance(layout)["color"] = "red"
			return p.body("未知字段", layout)
		}},
		{"unknown request field", func(p *canvasPanel) any {
			body := p.body("未知请求字段", p.layout())
			body["owner"] = "someone"
			return body
		}},
		{"name too long", func(p *canvasPanel) any {
			return p.body(strings.Repeat("方", canvasMaxNameBytes), p.layout())
		}},
		{"invalid execution", func(p *canvasPanel) any {
			body := p.body("坏执行位置", p.layout())
			body["execution"] = "browser"
			return body
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			p := newCanvasPanel(t)
			cookie, csrf := p.session("Owner")
			response := p.call("POST", "/api/v1/canvas-schemes", test.build(p), cookie, csrf)
			if response.Code != 400 {
				t.Fatalf("%s: %d %s", test.name, response.Code, response.Body.String())
			}
			if p.schemeCount() != 0 {
				t.Fatalf("%s: the refused drawing was stored", test.name)
			}
		})
	}
}

// TestCanvasSchemeDeleteLeavesProxyObjects checks the promise of this step:
// removing a drawing deletes the drawing and nothing else.
func TestCanvasSchemeDeleteLeavesProxyObjects(t *testing.T) {
	p := newCanvasPanel(t)
	cookie, csrf := p.session("Owner")
	outbound, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "A to B", NodeID: p.nodeA, Protocol: "socks5", Enabled: true, TargetInboundID: p.inbound.ID, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	counts := func() (int, int) {
		p.t.Helper()
		var inbounds, outbounds int
		if err := p.database.QueryRowContext(p.ctx, "SELECT (SELECT count(*) FROM inbounds),(SELECT count(*) FROM outbounds)").Scan(&inbounds, &outbounds); err != nil {
			p.t.Fatal(err)
		}
		return inbounds, outbounds
	}
	inboundsBefore, outboundsBefore := counts()
	layout := p.layout()
	layout["instances"] = append(layout["instances"].([]any), map[string]any{"instanceId": "inst-c", "ref": "import:manual-jp", "x": 720, "y": 200, "role": "exit"})
	created := p.create("引用现有对象的方案", layout, cookie, csrf)
	if response := p.call("DELETE", "/api/v1/canvas-schemes/"+created.ID, nil, cookie, csrf); response.Code != 204 {
		t.Fatalf("delete scheme: %d %s", response.Code, response.Body.String())
	}
	inboundsAfter, outboundsAfter := counts()
	if inboundsAfter != inboundsBefore || outboundsAfter != outboundsBefore || outboundsAfter != 1 {
		t.Fatalf("deleting a scheme changed proxy objects: inbounds %d->%d, outbounds %d->%d", inboundsBefore, inboundsAfter, outboundsBefore, outboundsAfter)
	}
	if _, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, false); err != nil {
		t.Fatalf("inbound disappeared: %v", err)
	}
	if _, err := p.service.Repository.Outbound(p.ctx, outbound.ID, false); err != nil {
		t.Fatalf("outbound disappeared: %v", err)
	}
}

// TestCanvasSchemeStoredLayoutIsTyped checks that a row which is not exactly the
// typed layout is refused instead of being served as JSON.
func TestCanvasSchemeStoredLayoutIsTyped(t *testing.T) {
	p := newCanvasPanel(t)
	cookie, csrf := p.session("Owner")
	created := p.create("类型校验", p.layout(), cookie, csrf)
	if _, err := p.database.ExecContext(p.ctx, "UPDATE canvas_schemes SET layout_json=? WHERE id=?", `{"instances":[{"instanceId":"inst-a","ref":"import:x","x":1,"y":2,"role":"entry","colour":"red"}],"connections":[]}`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.CanvasScheme(p.ctx, created.ID); err == nil {
		t.Fatal("unknown stored field accepted")
	}
	if response := p.call("GET", "/api/v1/canvas-schemes/"+created.ID, nil, cookie, ""); response.Code != 500 {
		t.Fatalf("unreadable stored layout: %d %s", response.Code, response.Body.String())
	}
}

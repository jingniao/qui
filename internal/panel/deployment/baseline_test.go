package deployment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (p *publishPanel) baseline(nodeID, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	p.t.Helper()
	mux := http.NewServeMux()
	p.handler.Register(mux)
	request := httptest.NewRequest("POST", "/api/v1/nodes/"+nodeID+"/baseline", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func (p *publishPanel) markOnline(nodeID string) {
	p.t.Helper()
	if _, err := p.database.ExecContext(p.ctx, "UPDATE nodes SET status='online',last_heartbeat=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), nodeID); err != nil {
		p.t.Fatal(err)
	}
}

// A node that never ran a configuration gets a baseline revision without any
// inbound, user or policy, and none of its drafts.
func TestPublishBaselineForUnpublishedNode(t *testing.T) {
	p := newPublishPanel(t)
	p.markOnline(p.nodeA)
	cookie, csrf := p.session("Owner")
	if response := p.baseline(p.nodeA, `{"confirmNodeId":"other"}`, cookie, csrf); response.Code != 400 {
		t.Fatalf("mismatched confirmation: %d %s", response.Code, response.Body.String())
	}
	if response := p.baseline(p.nodeB, `{"confirmNodeId":"`+p.nodeB+`"}`, cookie, csrf); response.Code != 409 || !strings.Contains(response.Body.String(), "node_offline") {
		t.Fatalf("offline node: %d %s", response.Code, response.Body.String())
	}
	response := p.baseline(p.nodeA, `{"confirmNodeId":"`+p.nodeA+`"}`, cookie, csrf)
	if response.Code != 202 {
		t.Fatalf("baseline: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	state, err := p.handler.Repository.State(p.ctx, body.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Inbounds) != 0 || len(state.Users) != 0 || len(state.Credentials) != 0 || len(state.Policies) != 0 || len(state.Outbounds) != 2 {
		t.Fatalf("baseline state carries configuration: %+v", state)
	}
	if target, err := p.handler.Repository.TargetStatus(p.ctx, p.nodeA, body.Revision); err != nil || target != "pending" {
		t.Fatalf("baseline target %q %v", target, err)
	}
	if len(p.audit.actions) != 1 || p.audit.actions[0] != "升级前发布基础配置" {
		t.Fatalf("audit %v", p.audit.actions)
	}

	// Once the node runs a revision the baseline is refused.
	if _, err := p.database.ExecContext(p.ctx, "INSERT INTO node_runtime_state(node_id,revision) VALUES(?,?)", p.nodeA, body.Revision); err != nil {
		t.Fatal(err)
	}
	if response := p.baseline(p.nodeA, `{"confirmNodeId":"`+p.nodeA+`"}`, cookie, csrf); response.Code != 409 || !strings.Contains(response.Body.String(), "baseline_not_needed") {
		t.Fatalf("published node: %d %s", response.Code, response.Body.String())
	}
}

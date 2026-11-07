package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func (p *reissuePanel) recoveryTokenRequest(t *testing.T, id, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("POST", "/api/v1/nodes/"+id+"/recovery-token", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	p.mux.ServeHTTP(response, request)
	return response
}

func (p *reissuePanel) recoveryStatusResponse(t *testing.T, id string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("GET", "/api/v1/nodes/"+id+"/recovery-status", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	p.mux.ServeHTTP(response, request)
	return response
}

// A recovery code hands over a node's whole configuration, so only the Owner may
// ask for one and only with a same-origin CSRF token.
func TestRecoveryTokenRequiresOwnerAndCSRF(t *testing.T) {
	p := newReissuePanel(t)
	ctx := context.Background()
	n := recoveryTestNode(t, p.repository, ctx)
	viewer, viewerCSRF := p.session(t, "Viewer")
	if response := p.recoveryTokenRequest(t, n.ID, `{}`, viewer, viewerCSRF); response.Code != 403 {
		t.Fatalf("viewer issued a recovery code: %d %s", response.Code, response.Body.String())
	}
	owner, _ := p.session(t, "Owner")
	if response := p.recoveryTokenRequest(t, n.ID, `{}`, owner, ""); response.Code != 403 || !strings.Contains(response.Body.String(), "csrf_failed") {
		t.Fatalf("recovery code issued without CSRF: %d %s", response.Code, response.Body.String())
	}
	if tokens := recoveryCount(t, p.repository, ctx, "SELECT count(*) FROM node_enrollment_tokens WHERE purpose='recover'"); tokens != 0 {
		t.Fatalf("refused requests left %d recovery codes", tokens)
	}
	if rows := auditRows(t, p.repository, ctx, "签发节点恢复码"); rows != 0 {
		t.Fatalf("refused requests wrote %d audit rows", rows)
	}
	if rows := auditRows(t, p.repository, ctx, "签发节点恢复码并吊销证书"); rows != 0 {
		t.Fatalf("refused requests wrote %d revocation audit rows", rows)
	}
}

// The lifetime is bounded and a code that is out of bounds is refused before the
// node is touched.
func TestRecoveryTokenValidatesLifetime(t *testing.T) {
	p := newReissuePanel(t)
	ctx := context.Background()
	n := recoveryTestNode(t, p.repository, ctx)
	owner, csrf := p.session(t, "Owner")
	for _, body := range []string{`{"expiresInSeconds":120}`, `{"expiresInSeconds":3601}`} {
		if response := p.recoveryTokenRequest(t, n.ID, body, owner, csrf); response.Code != 400 {
			t.Fatalf("%s: %d %s", body, response.Code, response.Body.String())
		}
	}
	if codes := recoveryCount(t, p.repository, ctx, "SELECT count(*) FROM node_identity_recoveries"); codes != 0 {
		t.Fatalf("a refused lifetime created %d recovery records", codes)
	}
	response := p.recoveryTokenRequest(t, n.ID, `{"expiresInSeconds":3600}`, owner, csrf)
	if response.Code != 201 {
		t.Fatalf("recovery code: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		EnrollmentToken string `json:"enrollmentToken"`
		Purpose         string `json:"purpose"`
		ExpiresAt       string `json:"expiresAt"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.EnrollmentToken == "" || body.Purpose != "recover" || body.ExpiresAt == "" {
		t.Fatalf("issued recovery: %+v", body)
	}
	if resolved, err := p.repository.EnrollmentNode(ctx, Hash(body.EnrollmentToken)); err != nil || resolved != n.ID {
		t.Fatalf("the issued code resolves to %q: %v", resolved, err)
	}
	if rows := auditRows(t, p.repository, ctx, "签发节点恢复码"); rows != 1 {
		t.Fatalf("audit rows: %d", rows)
	}
}

// The recent recovery is readable by the Owner only.
func TestRecoveryStatusRequiresOwner(t *testing.T) {
	p := newReissuePanel(t)
	ctx := context.Background()
	n := recoveryTestNode(t, p.repository, ctx)
	owner, csrf := p.session(t, "Owner")
	if response := p.recoveryTokenRequest(t, n.ID, `{}`, owner, csrf); response.Code != 201 {
		t.Fatalf("recovery code: %d %s", response.Code, response.Body.String())
	}
	response := p.recoveryStatusResponse(t, n.ID, owner)
	if response.Code != 200 {
		t.Fatalf("owner status: %d %s", response.Code, response.Body.String())
	}
	var status Recovery
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Status != "issued" || status.NodeID != n.ID || status.ExpiresAt == "" {
		t.Fatalf("status: %+v", status)
	}
	viewer, _ := p.session(t, "Viewer")
	if response := p.recoveryStatusResponse(t, n.ID, viewer); response.Code != 403 {
		t.Fatalf("viewer read the recovery status: %d %s", response.Code, response.Body.String())
	}
	if response := p.recoveryStatusResponse(t, "missing-node", owner); response.Code != 404 {
		t.Fatalf("unknown node: %d %s", response.Code, response.Body.String())
	}
}

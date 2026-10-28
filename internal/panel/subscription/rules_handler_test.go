package subscription

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
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/subscription/rules"
)

type rulesAudit struct{ actions []string }

func (a *rulesAudit) Record(_ context.Context, _, action, _, _, _ string) error {
	a.actions = append(a.actions, action)
	return nil
}

type stubServiceRules map[string]ServiceRule

func (s stubServiceRules) ServiceRule(_ context.Context, id string) (ServiceRule, error) {
	rule, ok := s[id]
	if !ok {
		return ServiceRule{}, ErrServiceRuleNotFound
	}
	return rule, nil
}

type acceptAll struct{}

func (acceptAll) ServiceSetExists(context.Context, string) bool { return true }
func (acceptAll) NodeGroupExists(context.Context, string) bool  { return true }
func (acceptAll) NodeExists(context.Context, string) bool       { return true }

// rulesPanel is the subscription handler on a real session middleware.
type rulesPanel struct {
	*fixture
	handler *Handler
	audit   *rulesAudit
	mux     *http.ServeMux
}

func newRulesPanel(t *testing.T) *rulesPanel {
	t.Helper()
	f := setup(t)
	audit := &rulesAudit{}
	store := &rules.Store{Database: f.database, Lookup: acceptAll{}}
	f.service.Rules = store
	handler := &Handler{
		Service:      f.service,
		Auth:         &auth.Handler{Service: &auth.Service{Repository: &auth.Repository{Database: f.database}, Settings: previewSettings{}, SessionDuration: time.Hour}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
		Audit:        audit,
		Rules:        store,
		ServiceRules: stubServiceRules{"svc-on": {Enabled: true, Domains: []string{"api.example.com", "bad,entry"}, Suffixes: []string{".example.org"}, CIDRs: []string{"203.0.113.0/24", "2001:db8::/32", "198.51.100.7", "nonsense"}}, "svc-off": {Enabled: false, Domains: []string{"x.example"}}},
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	return &rulesPanel{fixture: f, handler: handler, audit: audit, mux: mux}
}

func sessionFor(t *testing.T, database *sql.DB, role string) (*http.Cookie, string) {
	t.Helper()
	digest := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	token := "session-" + strings.ToLower(role)
	stamp := time.Now().UTC().Format(timestampLayout)
	if _, err := database.Exec("INSERT INTO admins(id,username,display_name,role,status,password_hash,created_at) VALUES(?,?,?,?,'active','unused',?)", role, role, role, role, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO sessions(token_hash,admin_id,expires_at,created_at) VALUES(?,?,?,?)", digest(token), role, time.Now().UTC().Add(time.Hour).Format(timestampLayout), stamp); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "proxy_session", Value: token}, digest("csrf:" + token)
}

func (p *rulesPanel) do(t *testing.T, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
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

func saveBody(t *testing.T, template rules.Template, expected int64) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"template": template, "expectedRevision": expected})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A2.2 and the API: the Owner saves, a stale revision gets 409, an invalid
// template gets 400 naming the field, a Viewer may read but not write.
func TestRulesAPI(t *testing.T) {
	p := newRulesPanel(t)
	owner, csrf := sessionFor(t, p.database, "Owner")
	viewer, viewerCSRF := sessionFor(t, p.database, "Viewer")

	read := p.do(t, "GET", "/api/v1/subscription-rules", "", viewer, "")
	var initial rules.Stored
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &initial) != nil || initial.Revision != 0 || initial.Template.Preset != "standard" {
		t.Fatalf("viewer read: %d %s", read.Code, read.Body.String())
	}
	if response := p.do(t, "GET", "/api/v1/subscription-rules/presets", "", viewer, ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"whitelist"`) {
		t.Fatalf("presets: %d", response.Code)
	}
	minimal := rules.Presets()[1].Template
	if response := p.do(t, "PUT", "/api/v1/subscription-rules", saveBody(t, minimal, 0), viewer, viewerCSRF); response.Code != 403 {
		t.Fatalf("viewer save: %d", response.Code)
	}
	saved := p.do(t, "PUT", "/api/v1/subscription-rules", saveBody(t, minimal, 0), owner, csrf)
	if saved.Code != 200 || !strings.Contains(saved.Body.String(), `"revision":1`) {
		t.Fatalf("owner save: %d %s", saved.Code, saved.Body.String())
	}
	if len(p.audit.actions) != 1 {
		t.Fatalf("audit %v", p.audit.actions)
	}
	if response := p.do(t, "PUT", "/api/v1/subscription-rules", saveBody(t, rules.Presets()[2].Template, 0), owner, csrf); response.Code != 409 || !strings.Contains(response.Body.String(), "stale_rules") {
		t.Fatalf("stale save: %d %s", response.Code, response.Body.String())
	}
	broken := rules.Presets()[2].Template
	broken.Rules[0].Payload = "a,b"
	response := p.do(t, "PUT", "/api/v1/subscription-rules", saveBody(t, broken, 1), owner, csrf)
	if response.Code != 400 || !strings.Contains(response.Body.String(), `"field":"rules[0].payload"`) {
		t.Fatalf("invalid save: %d %s", response.Code, response.Body.String())
	}
	if response := p.do(t, "PUT", "/api/v1/subscription-rules", `{"template":{"unknown":1},"expectedRevision":1}`, owner, csrf); response.Code != 400 {
		t.Fatalf("unknown field: %d", response.Code)
	}
}

// AU7.1: the rule provider is authenticated by the subscription token, follows
// the token's state, and serves classical lines with nothing injectable.
func TestRuleProviderEndpoint(t *testing.T) {
	p := newRulesPanel(t)
	p.publish(t, p.nodeIDs[0])
	_, plaintext, err := p.service.Rotate(context.Background(), p.u.ID, p.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	response := p.do(t, "GET", "/sub/"+plaintext+"/rules/svc-on", "", nil, "")
	want := "DOMAIN,api.example.com\nDOMAIN-SUFFIX,example.org\nIP-CIDR,203.0.113.0/24,no-resolve\nIP-CIDR6,2001:db8::/32,no-resolve\nIP-CIDR,198.51.100.7/32,no-resolve\n"
	if response.Code != 200 || response.Body.String() != want || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("provider: %d %q", response.Code, response.Body.String())
	}
	if response := p.do(t, "GET", "/sub/"+plaintext+"/rules/svc-off", "", nil, ""); response.Code != 404 {
		t.Fatalf("disabled service: %d", response.Code)
	}
	if response := p.do(t, "GET", "/sub/"+plaintext+"/rules/missing", "", nil, ""); response.Code != 404 {
		t.Fatalf("missing service: %d", response.Code)
	}
	if response := p.do(t, "GET", "/sub/not-a-token/rules/svc-on", "", nil, ""); response.Code != 403 {
		t.Fatalf("unknown token: %d", response.Code)
	}
	if _, _, err := p.service.Rotate(context.Background(), p.u.ID, p.device.ID); err != nil {
		t.Fatal(err)
	}
	if response := p.do(t, "GET", "/sub/"+plaintext+"/rules/svc-on", "", nil, ""); response.Code != 403 {
		t.Fatalf("revoked token: %d", response.Code)
	}
}

// The preview renders a whole document per format for the Owner, with its
// warnings, and never for a Viewer.
func TestPreviewDocumentByFormat(t *testing.T) {
	p := newRulesPanel(t)
	p.publish(t, p.nodeIDs[0])
	owner, _ := sessionFor(t, p.database, "Owner")
	viewer, _ := sessionFor(t, p.database, "Viewer")
	path := "/api/v1/users/" + p.u.ID + "/subscription-preview?deviceId=" + p.device.ID + "&format=uri"
	response := p.do(t, "GET", path, "", owner, "")
	var body struct {
		Format, ContentType, Body string
		Warnings                  []previewWarning
	}
	// The document lists the user's http and socks5 credentials. Their order comes
	// from the published revision, not from the old device fixture, so the test
	// checks the content of both lines instead of a fixed first line.
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.ContentType == "" ||
		!strings.Contains(body.Body, "http://device-") || !strings.Contains(body.Body, "socks5://device-") {
		t.Fatalf("preview: %d %s", response.Code, response.Body.String())
	}
	if response := p.do(t, "GET", strings.Replace(path, "format=uri", "format=nope", 1), "", owner, ""); response.Code != 400 {
		t.Fatalf("unknown format: %d", response.Code)
	}
	if response := p.do(t, "GET", path, "", viewer, ""); response.Code != 403 {
		t.Fatalf("viewer preview: %d", response.Code)
	}
	var fetched string
	if err := p.database.QueryRow("SELECT coalesce(max(last_fetched_at),'') FROM subscription_tokens").Scan(&fetched); err != nil {
		t.Fatal(err)
	}
	if fetched != "" {
		t.Fatal("the preview marked a token fetched")
	}
}

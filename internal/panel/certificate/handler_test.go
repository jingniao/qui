package certificate

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
)

// setupSettings is the two-method platform settings interface the authentication
// service needs. The certificate package's tests cannot import the system package
// (that closes a cycle through the backup package), so the first-run rows are
// written here in the same shape the panel uses.
type setupSettings struct{}

func (setupSettings) SaveSetup(ctx context.Context, tx *sql.Tx, name, timezone string) error {
	for key, value := range map[string]string{"platform_name": name, "timezone": timezone} {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?)", key, string(data)); err != nil {
			return err
		}
	}
	return nil
}

func (setupSettings) ReadSetup(context.Context) (string, string, error) {
	return "Certificate Panel", "UTC", nil
}

// certificateHTTP wires the real authentication middleware in front of the
// certificate handler, so the Owner-only rule, the CSRF requirement and every
// documented status code are checked end to end.
type certificateHTTP struct {
	handler    http.Handler
	service    *Service
	repository *Repository
	dns        *fakeDNS
	database   *sql.DB
	nodeID     string
	owner      string
	ownerCSRF  string
	viewer     string
	viewerCSRF string
}

func newCertificateHTTP(t *testing.T) *certificateHTTP {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(root, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(root, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{Database: database, Vault: key}
	audit := &fakeAuditor{now: time.Now}
	dns := &fakeDNS{zones: map[string]Zone{"example.com": {ID: "zone-example", Name: "example.com", NameServers: []string{"ns1.example.com"}}}}
	service := NewService(repository)
	service.Issuer = &fakeIssuer{order: newIssuerOrder()}
	service.DNS = dns
	service.Propagation = &fakePropagation{}
	service.Audit = audit
	service.Now = time.Now
	authService := &auth.Service{Repository: &auth.Repository{Database: database}, Audit: audit, Settings: setupSettings{}, SessionDuration: 2 * time.Hour}
	const token, password = "certificate-setup-token", "certificate-password-123"
	tokenPath := filepath.Join(t.TempDir(), "setup-token")
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	authService.SetSetupToken(token, tokenPath)
	ownerToken, ownerSession, err := authService.Setup(ctx, token, "owner", password, "Certificate Panel", "UTC", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authService.CreateAdmin(ctx, ownerSession.CurrentUser, "viewer", "Viewer", password, "Viewer", "192.0.2.11"); err != nil {
		t.Fatal(err)
	}
	viewerToken, viewerSession, err := authService.Login(ctx, "viewer", password, "192.0.2.12")
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{Service: service, Auth: &auth.Handler{Service: authService, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
		Actor: func(ctx context.Context) string { return auth.Actor(ctx).Username }}
	mux := http.NewServeMux()
	handler.Register(mux)
	return &certificateHTTP{handler: mux, service: service, repository: repository, dns: dns, database: database,
		owner: ownerToken, ownerCSRF: ownerSession.CSRFToken, viewer: viewerToken, viewerCSRF: viewerSession.CSRFToken}
}

// call sends one request and returns the recorded response.
func (f *certificateHTTP) call(t *testing.T, method, path, body, cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.10:4000"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: "proxy_session", Value: cookie})
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func TestCertificateSettingsRequireOwnerAndHideTheToken(t *testing.T) {
	fixture := newCertificateHTTP(t)
	const path = "/api/v1/system/certificate-settings"
	if code := fixture.call(t, "GET", path, "", "", "").Code; code != 401 {
		t.Fatalf("anonymous GET: %d", code)
	}
	if code := fixture.call(t, "GET", path, "", fixture.viewer, "").Code; code != 403 {
		t.Fatalf("viewer GET: %d", code)
	}
	if code := fixture.call(t, "PUT", path, `{"email":"owner@example.com","expectedRevision":0}`, fixture.viewer, fixture.viewerCSRF).Code; code != 403 {
		t.Fatalf("viewer PUT: %d", code)
	}
	body := `{"email":"owner@example.com","directory":"staging","cloudflareToken":"cloudflare-token-wxyz","expectedRevision":0}`
	if code := fixture.call(t, "PUT", path, body, fixture.owner, "").Code; code != 403 {
		t.Fatalf("owner PUT without CSRF: %d", code)
	}
	response := fixture.call(t, "PUT", path, body, fixture.owner, fixture.ownerCSRF)
	if response.Code != 200 {
		t.Fatalf("owner PUT: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "cloudflare-token-wxyz") {
		t.Fatalf("the token was returned: %s", response.Body.String())
	}
	var saved SettingsView
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Email != "owner@example.com" || saved.Directory != DirectoryStaging || saved.Revision != 1 || !saved.Cloudflare.Configured || saved.Cloudflare.Last4 != "wxyz" {
		t.Fatalf("saved settings: %+v", saved)
	}
	read := fixture.call(t, "GET", path, "", fixture.owner, "")
	if read.Code != 200 || strings.Contains(read.Body.String(), "cloudflare-token-wxyz") {
		t.Fatalf("owner GET: %d %s", read.Code, read.Body.String())
	}
	// A stale revision is a conflict, and an unknown field is a bad request.
	if stale := fixture.call(t, "PUT", path, `{"email":"other@example.com","directory":"staging","expectedRevision":0}`, fixture.owner, fixture.ownerCSRF); stale.Code != 409 || !strings.Contains(stale.Body.String(), "settings_conflict") {
		t.Fatalf("stale PUT: %d %s", stale.Code, stale.Body.String())
	}
	if invalid := fixture.call(t, "PUT", path, `{"email":"owner@example.com","directory":"staging","expectedRevision":1,"token":"x"}`, fixture.owner, fixture.ownerCSRF); invalid.Code != 400 {
		t.Fatalf("unknown field: %d", invalid.Code)
	}
	// A rejected token is refused with its own code and is not stored.
	fixture.dns.verifyErr = fail(CodeCloudflareTokenInvalid)
	rejected := fixture.call(t, "PUT", path, `{"email":"owner@example.com","directory":"staging","expectedRevision":1,"cloudflareToken":"bad-token"}`, fixture.owner, fixture.ownerCSRF)
	if rejected.Code != 400 || !strings.Contains(rejected.Body.String(), "cloudflare_token_invalid") {
		t.Fatalf("invalid token: %d %s", rejected.Code, rejected.Body.String())
	}
	if _, status, err := fixture.repository.ReadCloudflare(context.Background()); err != nil || !status.Configured || status.Last4 != "wxyz" {
		t.Fatalf("the rejected save changed the token: %+v %v", status, err)
	}
	// Clearing the token keeps the email and drops the credential.
	fixture.dns.verifyErr = nil
	cleared := fixture.call(t, "PUT", path, `{"email":"owner@example.com","directory":"staging","cloudflareToken":"","expectedRevision":1}`, fixture.owner, fixture.ownerCSRF)
	if cleared.Code != 200 || strings.Contains(cleared.Body.String(), `"configured":true`) {
		t.Fatalf("cleared settings: %d %s", cleared.Code, cleared.Body.String())
	}
}

func TestCertificateRoutesValidateAndQueue(t *testing.T) {
	fixture := newCertificateHTTP(t)
	ctx := context.Background()
	// Without an email and a token, an issuance request is refused.
	if missing := fixture.call(t, "POST", "/api/v1/certificates", `{"domains":["edge.example.com"]}`, fixture.owner, fixture.ownerCSRF); missing.Code != 400 || !strings.Contains(missing.Body.String(), "certificate_settings_missing") {
		t.Fatalf("missing settings: %d %s", missing.Code, missing.Body.String())
	}
	token := testToken
	if _, _, err := fixture.repository.SaveSettings(ctx, SaveSettingsInput{Email: "owner@example.com", Directory: DirectoryStaging, Token: &token, VerifiedAt: Stamp(time.Now()), Actor: "owner"}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"domains":[]}`,
		`{"domains":["192.0.2.1"]}`,
		`{"domains":["localhost"]}`,
		`{"domains":["edge.example.com","edge.example.com"]}`,
		`{"domains":["*.","edge.example.com"]}`,
		`{"domains":["edge.example.com"],"autoDns":true}`,
		`{"domains":["edge.example.com"],"autoDns":true,"dnsNodeId":"unknown"}`,
		`{"domains":["edge.example.com"],"unknown":1}`,
	} {
		if code := fixture.call(t, "POST", "/api/v1/certificates", body, fixture.owner, fixture.ownerCSRF).Code; code != 400 {
			t.Fatalf("body %s: %d", body, code)
		}
	}
	if code := fixture.call(t, "POST", "/api/v1/certificates", `{"domains":["edge.example.com"]}`, fixture.viewer, fixture.viewerCSRF).Code; code != 403 {
		t.Fatalf("viewer POST: %d", code)
	}
	created := fixture.call(t, "POST", "/api/v1/certificates", `{"domains":["*.Edge.Example.com","edge.example.com"]}`, fixture.owner, fixture.ownerCSRF)
	if created.Code != 202 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var view View
	if err := json.Unmarshal(created.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Status != StatusPending || len(view.Domains) != 2 || view.Domains[0] != "*.edge.example.com" || view.Domains[1] != "edge.example.com" || len(view.UsedBy) != 0 {
		t.Fatalf("created view: %+v", view)
	}
	list := fixture.call(t, "GET", "/api/v1/certificates", "", fixture.owner, "")
	if list.Code != 200 || !strings.Contains(list.Body.String(), view.ID) {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	if code := fixture.call(t, "POST", "/api/v1/certificates/"+view.ID+"/renew", "", fixture.owner, fixture.ownerCSRF).Code; code != 202 {
		t.Fatalf("renew: %d", code)
	}
	if code := fixture.call(t, "POST", "/api/v1/certificates/missing/renew", "", fixture.owner, fixture.ownerCSRF).Code; code != 404 {
		t.Fatalf("renew unknown: %d", code)
	}
	if code := fixture.call(t, "DELETE", "/api/v1/certificates/"+view.ID, "", fixture.owner, fixture.ownerCSRF).Code; code != 204 {
		t.Fatalf("delete: %d", code)
	}
	audit := fixture.service.Audit.(*fakeAuditor)
	if writes := audit.actions("申请证书") + audit.actions("续期证书") + audit.actions("删除证书"); writes != 3 {
		t.Fatalf("audit rows: %d %v", writes, audit.entries)
	}
}

func TestCertificateInUseIsRefused(t *testing.T) {
	fixture := newCertificateHTTP(t)
	ctx := context.Background()
	nodes := &node.Repository{Database: fixture.database}
	created, _, _, err := nodes.CreateEnrollment(ctx, "edge", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	stamp := Stamp(time.Now())
	identifier, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.Create(ctx, Certificate{ID: identifier, Domains: []string{"edge.example.com"}, Status: StatusPending, CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at,certificate_id)
VALUES('inbound-1','Hysteria2',?,'hysteria2','0.0.0.0',8443,1,1,0,'direct','[]',x'01',?,?)`, created.ID, stamp, identifier); err != nil {
		t.Fatal(err)
	}
	list := fixture.call(t, "GET", "/api/v1/certificates", "", fixture.owner, "")
	if list.Code != 200 || !strings.Contains(list.Body.String(), "inbound-1") || !strings.Contains(list.Body.String(), "Hysteria2") {
		t.Fatalf("usedBy: %d %s", list.Code, list.Body.String())
	}
	refused := fixture.call(t, "DELETE", "/api/v1/certificates/"+identifier, "", fixture.owner, fixture.ownerCSRF)
	if refused.Code != 409 || !strings.Contains(refused.Body.String(), "certificate_in_use") || !strings.Contains(refused.Body.String(), "Hysteria2") {
		t.Fatalf("delete in use: %d %s", refused.Code, refused.Body.String())
	}
	if _, err := fixture.repository.Get(ctx, identifier); err != nil {
		t.Fatalf("the refused deletion removed the row: %v", err)
	}
}

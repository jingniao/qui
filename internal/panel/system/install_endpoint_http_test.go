package system

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/db"
	"strings"
	"testing"
	"time"
)

// installAudit is the minimal operation-log sink the authentication service
// requires; this feature writes its audit row inside its own transaction.
type installAudit struct{}

func (installAudit) Record(context.Context, string, string, string, string, string) error {
	return nil
}

type installHTTPFixture struct {
	handler            http.Handler
	repository         *Repository
	owner, ownerCSRF   string
	viewer, viewerCSRF string
}

// newInstallHTTPFixture wires the real authentication middleware in front of the
// system handler so the Owner-only rule, the CSRF requirement and the response
// codes are checked end to end, the same way the base settings expect it.
func newInstallHTTPFixture(t *testing.T) *installHTTPFixture {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	repository := &Repository{Database: database}
	service := &auth.Service{Repository: &auth.Repository{Database: database}, Audit: installAudit{}, Settings: repository, SessionDuration: 2 * time.Hour}
	const token, password = "install-setup-token", "install-password-123"
	tokenPath := filepath.Join(t.TempDir(), "setup-token")
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service.SetSetupToken(token, tokenPath)
	ownerToken, session, err := service.Setup(ctx, token, "owner", password, "Install Panel", "Asia/Shanghai", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateAdmin(ctx, session.CurrentUser, "viewer", "Viewer", password, "Viewer", "192.0.2.11"); err != nil {
		t.Fatal(err)
	}
	viewerToken, viewerSession, err := service.Login(ctx, "viewer", password, "192.0.2.12")
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{Repository: repository, Auth: &auth.Handler{Service: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
		ActorName: func(ctx context.Context) string { return auth.Actor(ctx).Username }}
	mux := http.NewServeMux()
	handler.Register(mux)
	return &installHTTPFixture{handler: mux, repository: repository, owner: ownerToken, ownerCSRF: session.CSRFToken, viewer: viewerToken, viewerCSRF: viewerSession.CSRFToken}
}

func (f *installHTTPFixture) do(t *testing.T, method, body, cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/system/install-endpoint", strings.NewReader(body))
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

func TestInstallEndpointHTTPOwnerOnlyCSRFAndConflict(t *testing.T) {
	f := newInstallHTTPFixture(t)
	status := func(method, body, cookie, csrf string) int {
		t.Helper()
		return f.do(t, method, body, cookie, csrf).Code
	}
	const valid = `{"host":"panel.example.com","httpPort":22550,"expectedRevision":0}`
	if code := status("GET", "", "", ""); code != 401 {
		t.Fatalf("unauthenticated GET: %d", code)
	}
	// Only the Owner sees the install dialogs, so the read is Owner-only too.
	if code := status("GET", "", f.viewer, ""); code != 403 {
		t.Fatalf("viewer GET: %d", code)
	}
	if code := status("PUT", valid, f.viewer, f.viewerCSRF); code != 403 {
		t.Fatalf("viewer PUT: %d", code)
	}
	if code := status("PUT", valid, f.owner, ""); code != 403 {
		t.Fatalf("owner PUT without CSRF: %d", code)
	}
	for _, body := range []string{
		`{"host":"2001:db8::1","httpPort":22550,"expectedRevision":0}`,
		`{"host":"panel.example.com","httpPort":18880,"expectedRevision":0,"extra":1}`,
		`{"host":"panel.example.com","httpPort":70000,"expectedRevision":0}`,
		`{"host":"https://panel.example.com","httpPort":22550,"expectedRevision":0}`,
	} {
		// A rejected request must be a 400 and must never store a value.
		if code := status("PUT", body, f.owner, f.ownerCSRF); code != 400 {
			t.Fatalf("invalid body %s: %d", body, code)
		}
	}
	if saved, err := f.repository.ReadInstallEndpoint(context.Background()); err != nil || saved.Revision != 0 || saved.Host != "" {
		t.Fatalf("rejected writes stored a value: %+v %v", saved, err)
	}
	// An all-empty body while nothing is saved is an unchanged save: it answers
	// the current (unset) value without a revision or an audit row.
	empty := f.do(t, "PUT", `{}`, f.owner, f.ownerCSRF)
	if empty.Code != 200 {
		t.Fatalf("unchanged empty PUT: %d %s", empty.Code, empty.Body.String())
	}
	var unset InstallEndpoint
	if err := json.Unmarshal(empty.Body.Bytes(), &unset); err != nil || unset.Revision != 0 || unset.Host != "" || unset.HTTPPort != 0 {
		t.Fatalf("unchanged empty PUT answered %+v %v", unset, err)
	}
	response := f.do(t, "PUT", valid, f.owner, f.ownerCSRF)
	if response.Code != 200 {
		t.Fatalf("owner PUT: %d %s", response.Code, response.Body.String())
	}
	var saved InstallEndpoint
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil || saved.Host != "panel.example.com" || saved.HTTPPort != 22550 || saved.Revision != 1 || saved.UpdatedBy != "owner" {
		t.Fatalf("saved endpoint: %+v %v", saved, err)
	}
	read := f.do(t, "GET", "", f.owner, "")
	if read.Code != 200 {
		t.Fatalf("owner GET: %d", read.Code)
	}
	var current InstallEndpoint
	if err := json.Unmarshal(read.Body.Bytes(), &current); err != nil || current != saved {
		t.Fatalf("read back: %+v %v", current, err)
	}
	// An unchanged save answers 200 with the stored revision and writes no audit.
	unchanged := f.do(t, "PUT", `{"host":"panel.example.com","httpPort":22550,"expectedRevision":1}`, f.owner, f.ownerCSRF)
	if unchanged.Code != 200 {
		t.Fatalf("unchanged PUT: %d %s", unchanged.Code, unchanged.Body.String())
	}
	if err := json.Unmarshal(unchanged.Body.Bytes(), &current); err != nil || current.Revision != 1 {
		t.Fatalf("unchanged save bumped the revision: %+v %v", current, err)
	}
	var audits int
	if err := f.repository.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='修改节点安装连接地址'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("audit rows: %d", audits)
	}
	// A stale view is refused with the same conflict code the base settings use.
	stale := f.do(t, "PUT", `{"host":"other.example.com","httpPort":443,"expectedRevision":0}`, f.owner, f.ownerCSRF)
	if stale.Code != 409 || !strings.Contains(stale.Body.String(), "settings_conflict") {
		t.Fatalf("stale PUT: %d %s", stale.Code, stale.Body.String())
	}
}

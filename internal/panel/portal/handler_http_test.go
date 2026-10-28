package portal_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/portal"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
)

const timestampLayout = "2006-01-02T15:04:05.000000000Z"

type setupStub struct{}

func (setupStub) SaveSetup(context.Context, *sql.Tx, string, string) error { return nil }
func (setupStub) ReadSetup(context.Context) (string, string, error) {
	return "Test Control", "UTC", nil
}

type auditRecorder struct{ actions []string }

func (a *auditRecorder) Record(_ context.Context, _, action, _, _, _ string) error {
	a.actions = append(a.actions, action)
	return nil
}

// panel is the portal handler on a real database with the real admin middleware.
type panel struct {
	handler  *portal.Handler
	mux      *http.ServeMux
	database *sql.DB
	users    *user.Repository
	audit    *auditRecorder
}

func newPanel(t *testing.T) *panel {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	configurationVault, err := vault.Open(filepath.Join(t.TempDir(), "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	audit := &auditRecorder{}
	handler := &portal.Handler{
		Repository: &portal.Repository{Database: database},
		Auth: &auth.Handler{
			Service: &auth.Service{Repository: &auth.Repository{Database: database}, Settings: setupStub{}, Audit: audit, SessionDuration: time.Hour},
			Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		Audit: audit,
	}
	mux := http.NewServeMux()
	handler.Service = &portal.Service{Repository: handler.Repository, Auth: handler.Auth.Service, Users: &user.Repository{Database: database, Vault: configurationVault}}
	handler.Register(mux)
	return &panel{handler: handler, mux: mux, database: database, users: &user.Repository{Database: database, Vault: configurationVault}, audit: audit}
}

func TestPortalSecureCookieAndPasswordValidation(t *testing.T) {
	p := newPanel(t)
	p.handler.Auth.Secure = true
	u := p.addUser(t, "secure_user")
	hash, err := auth.HashPassword("secure-user-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.handler.Repository.SetLogin(context.Background(), u.ID, true, hash); err != nil {
		t.Fatal(err)
	}
	r := p.do(t, "POST", "/api/v1/portal/auth/login", `{"username":"secure_user","password":"secure-user-password-1"}`, nil, "")
	if r.Code != 200 {
		t.Fatalf("login: %d %s", r.Code, r.Body.String())
	}
	cookie := r.Result().Cookies()[0]
	if cookie.Name != "__Host-qui_portal_session" || !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("secure cookie: %+v", cookie)
	}
	csrf := auth.CSRF(cookie.Value)
	for _, body := range []string{`{"currentPassword":"wrong","newPassword":"valid-new-password-2"}`, `{"currentPassword":"secure-user-password-1","newPassword":"short"}`} {
		r := p.do(t, "PUT", "/api/v1/portal/auth/password", body, cookie, csrf)
		if r.Code != 401 && r.Code != 400 {
			t.Fatalf("invalid password accepted: %d", r.Code)
		}
	}
	if r := p.do(t, "GET", "/api/v1/portal/me", "", &http.Cookie{Name: "qui_portal_session", Value: cookie.Value}, ""); r.Code != 401 {
		t.Fatal("insecure cookie accepted by secure handler")
	}
	if r := p.do(t, "POST", "/api/v1/portal/auth/logout", "", cookie, csrf); r.Code != 204 || r.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("secure logout: %d", r.Code)
	}
}

func (p *panel) do(t *testing.T, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.30:4444"
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

// sessionFor signs an administrator in without the login flow: the portal tests
// only need the middleware's view of a session.
func sessionFor(t *testing.T, database *sql.DB, role string) (*http.Cookie, string) {
	t.Helper()
	token := "session-" + strings.ToLower(role)
	stamp := time.Now().UTC().Format(timestampLayout)
	if _, err := database.Exec("INSERT INTO admins(id,username,display_name,role,status,password_hash,created_at) VALUES(?,?,?,?,'active','unused',?)", role, role, role, role, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO sessions(token_hash,admin_id,expires_at,created_at) VALUES(?,?,?,?)", auth.HashToken(token), role, time.Now().UTC().Add(time.Hour).Format(timestampLayout), stamp); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "proxy_session", Value: token}, auth.CSRF(token)
}

func (p *panel) addUser(t *testing.T, username string) user.User {
	t.Helper()
	account, err := p.users.Save(context.Background(), "", user.Input{Username: username, DisplayName: username, Status: "active", AllowedProtocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func (p *panel) loginColumns(t *testing.T, userID string) (string, bool) {
	t.Helper()
	var hash string
	var enabled int
	if err := p.database.QueryRow("SELECT password_hash,login_enabled FROM users WHERE id=?", userID).Scan(&hash, &enabled); err != nil {
		t.Fatal(err)
	}
	return hash, enabled == 1
}

func (p *panel) sessionCount(t *testing.T, userID string) int {
	t.Helper()
	var count int
	if err := p.database.QueryRow("SELECT count(*) FROM user_sessions WHERE user_id=?", userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

var sessionCounter int

func (p *panel) addSession(t *testing.T, userID string) {
	t.Helper()
	sessionCounter++
	if _, err := p.database.Exec("INSERT INTO user_sessions(token_hash,user_id,expires_at,created_at) VALUES(?,?,?,?)", auth.HashToken("portal-"+userID+"-"+string(rune('a'+sessionCounter%26))+time.Now().Format("150405.000")), userID, time.Now().UTC().Add(time.Hour).Format(timestampLayout), time.Now().UTC().Format(timestampLayout)); err != nil {
		t.Fatal(err)
	}
}

// A6.4 (the admin half): setting a password enables the login, disabling it or
// replacing the password ends the user's portal sessions immediately, the
// revoke-sessions route clears them, the password rules are the admins' own, and
// an unknown user, a Viewer and a caller without CSRF are refused.
func TestOwnerManagesUserLoginAndSessions(t *testing.T) {
	p := newPanel(t)
	owner, csrf := sessionFor(t, p.database, "Owner")
	viewer, viewerCSRF := sessionFor(t, p.database, "Viewer")
	account := p.addUser(t, "portal_user")
	loginPath := "/api/v1/users/" + account.ID + "/login"
	sessionsPath := "/api/v1/users/" + account.ID + "/sessions"

	if response := p.do(t, "PUT", loginPath, `{"enabled":true}`, owner, csrf); response.Code != 400 || !strings.Contains(response.Body.String(), "password") {
		t.Fatalf("enabling without a password: %d %s", response.Code, response.Body.String())
	}
	if response := p.do(t, "PUT", loginPath, `{"enabled":true,"password":"short"}`, owner, csrf); response.Code != 400 {
		t.Fatalf("weak password: %d %s", response.Code, response.Body.String())
	}
	if response := p.do(t, "PUT", loginPath, `{"enabled":true,"password":"portal-user-password-1"}`, owner, csrf); response.Code != 204 {
		t.Fatalf("enable: %d %s", response.Code, response.Body.String())
	}
	hash, enabled := p.loginColumns(t, account.ID)
	if !enabled || !auth.VerifyPassword(hash, "portal-user-password-1") {
		t.Fatalf("login was not enabled with the given password: enabled=%v hash=%q", enabled, hash)
	}
	if response := p.do(t, "PUT", loginPath, `{"enabled":true}`, owner, csrf); response.Code != 204 {
		t.Fatalf("re-enable without a new password: %d %s", response.Code, response.Body.String())
	}

	// A password reset ends the sessions so the reset takes effect immediately.
	p.addSession(t, account.ID)
	if response := p.do(t, "PUT", loginPath, `{"enabled":true,"password":"portal-user-password-2"}`, owner, csrf); response.Code != 204 {
		t.Fatalf("reset password: %d %s", response.Code, response.Body.String())
	}
	if count := p.sessionCount(t, account.ID); count != 0 {
		t.Fatalf("the password reset left %d sessions", count)
	}
	hash, _ = p.loginColumns(t, account.ID)
	if !auth.VerifyPassword(hash, "portal-user-password-2") {
		t.Fatal("the new password does not verify")
	}

	// Disabling ends them too, and clears nothing else.
	p.addSession(t, account.ID)
	if response := p.do(t, "PUT", loginPath, `{"enabled":false}`, owner, csrf); response.Code != 204 {
		t.Fatalf("disable: %d %s", response.Code, response.Body.String())
	}
	if _, enabled := p.loginColumns(t, account.ID); enabled {
		t.Fatal("the login is still enabled")
	}
	if count := p.sessionCount(t, account.ID); count != 0 {
		t.Fatalf("disabling the login left %d sessions", count)
	}

	// The explicit revoke route clears whatever is open.
	p.addSession(t, account.ID)
	p.addSession(t, account.ID)
	if response := p.do(t, "DELETE", sessionsPath, "", owner, csrf); response.Code != 204 {
		t.Fatalf("revoke sessions: %d %s", response.Code, response.Body.String())
	}
	if count := p.sessionCount(t, account.ID); count != 0 {
		t.Fatalf("the revoke route left %d sessions", count)
	}

	if response := p.do(t, "PUT", "/api/v1/users/missing/login", `{"enabled":false}`, owner, csrf); response.Code != 404 {
		t.Fatalf("unknown user: %d", response.Code)
	}
	if response := p.do(t, "DELETE", "/api/v1/users/missing/sessions", "", owner, csrf); response.Code != 404 {
		t.Fatalf("unknown sessions target: %d", response.Code)
	}
	if response := p.do(t, "PUT", loginPath, `{"enabled":true}`, viewer, viewerCSRF); response.Code != 403 {
		t.Fatalf("viewer enable: %d", response.Code)
	}
	if response := p.do(t, "PUT", loginPath, `{"enabled":true,"password":"portal-user-password-3"}`, owner, ""); response.Code != 403 {
		t.Fatalf("no CSRF: %d", response.Code)
	}
	if response := p.do(t, "PUT", loginPath, `{"enabled":true,"unknown":1}`, owner, csrf); response.Code != 400 {
		t.Fatalf("unknown field: %d %s", response.Code, response.Body.String())
	}

	want := []string{"设置用户登录密码", "启用用户登录", "设置用户登录密码", "停用用户登录", "撤销用户登录会话"}
	if strings.Join(p.audit.actions, "|") != strings.Join(want, "|") {
		t.Fatalf("audit actions %v, want %v", p.audit.actions, want)
	}
}

// A6.4 (the cascade half): deleting a user removes its sessions through the
// foreign key, so a stale cookie cannot resolve to a deleted account.
func TestDeletingAUserEndsItsSessions(t *testing.T) {
	p := newPanel(t)
	account := p.addUser(t, "cascade_user")
	p.addSession(t, account.ID)
	if count := p.sessionCount(t, account.ID); count != 1 {
		t.Fatalf("setup left %d sessions", count)
	}
	if err := p.users.Delete(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if count := p.sessionCount(t, account.ID); count != 0 {
		t.Fatalf("deleting the user left %d sessions", count)
	}
}

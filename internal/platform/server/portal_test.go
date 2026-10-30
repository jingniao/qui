package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/portal"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
)

const portalPassword = "portal-original-password-1"

func portalUser(t *testing.T, app *Server, owner budgetHTTP, name string) user.User {
	t.Helper()
	r := owner.call("POST", "/api/v1/users", map[string]any{"username": name, "displayName": name, "status": "active", "allowedProtocols": []string{"socks5"}})
	var u user.User
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &u) != nil {
		t.Fatalf("create user: %d %s", r.Code, r.Body.String())
	}
	if r := owner.call("PUT", "/api/v1/users/"+u.ID+"/login", map[string]any{"enabled": true, "password": portalPassword}); r.Code != 204 {
		t.Fatalf("enable login: %d %s", r.Code, r.Body.String())
	}
	return u
}

func portalLogin(t *testing.T, app *Server, username, password string) budgetHTTP {
	t.Helper()
	c := budgetHTTP{app: app}
	r := c.call("POST", "/api/v1/portal/auth/login", map[string]string{"username": username, "password": password})
	var view portal.Session
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &view) != nil || !view.IsAuthenticated {
		t.Fatalf("portal login: %d %s", r.Code, r.Body.String())
	}
	c.cookie, c.csrf = r.Result().Cookies()[0], view.CSRFToken
	return c
}

// registeredPatterns reads the actual ServeMux routing tree after registration.
// Reflection stays test-only, uses no unsafe, and is pinned to the repository's
// Go toolchain. A changed stdlib layout fails loudly instead of losing coverage.
func registeredPatterns(t *testing.T, mux *http.ServeMux) []string {
	t.Helper()
	var patterns []string
	var visit func(reflect.Value)
	visit = func(v reflect.Value) {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return
			}
			v = v.Elem()
		}
		if !v.IsValid() || v.Kind() != reflect.Struct {
			return
		}
		p := v.FieldByName("pattern")
		if !p.IsNil() {
			patterns = append(patterns, p.Elem().FieldByName("str").String())
		}
		visit(v.FieldByName("multiChild"))
		visit(v.FieldByName("emptyChild"))
		children := v.FieldByName("children")
		s := children.FieldByName("s")
		for i := 0; i < s.Len(); i++ {
			visit(s.Index(i).FieldByName("value"))
		}
		m := children.FieldByName("m").MapRange()
		for m.Next() {
			visit(m.Value())
		}
	}
	root := reflect.ValueOf(mux).Elem().FieldByName("tree")
	if !root.IsValid() {
		t.Fatal("ServeMux layout changed: update route inventory reader")
	}
	visit(root)
	sort.Strings(patterns)
	return patterns
}

func coveragePatterns(t *testing.T, app *Server) []string {
	t.Helper()
	data, err := os.ReadFile("testdata/portal_route_coverage.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSpace(string(data)), "\n")
	got := registeredPatterns(t, app.httpMux)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registered routes changed; classify and test each new route in portal_route_coverage.txt\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	return got
}

func routeCall(c budgetHTTP, pattern string) *httptest.ResponseRecorder {
	parts := strings.SplitN(pattern, " ", 2)
	method, path := "GET", parts[0]
	if len(parts) == 2 {
		method, path = parts[0], parts[1]
	}
	path = regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(path, "foreign-id")
	if pattern == "/api/" {
		path = "/api/v1/unregistered"
	}
	return c.call(method, path, map[string]string{})
}

func TestEveryAdminRouteRefusesAPortalSession(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	u := portalUser(t, app, owner, "route_user")
	c := portalLogin(t, app, u.Username, portalPassword)
	public := map[string]bool{"GET /api/v1/auth/session": true, "POST /api/v1/auth/login": true, "POST /api/v1/auth/setup": true}
	for _, pattern := range coveragePatterns(t, app) {
		if (!strings.Contains(pattern, "/api/") || strings.Contains(pattern, "/portal/")) || public[pattern] {
			continue
		}
		t.Run(pattern, func(t *testing.T) {
			if r := routeCall(c, pattern); r.Code != 401 && r.Code != 403 {
				t.Fatalf("wrong principal: %d %s", r.Code, r.Body.String())
			}
		})
	}
	// Public bootstrap endpoints accept credentials, not a session principal.
	if r := c.call("GET", "/api/v1/auth/session", nil); strings.Contains(r.Body.String(), `"isAuthenticated":true`) {
		t.Fatal("portal cookie authenticated as admin")
	}
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/setup"} {
		if r := c.call("POST", path, map[string]string{"username": u.Username, "password": portalPassword}); r.Code < 400 {
			t.Fatalf("portal cookie bypassed bootstrap authentication: %s", path)
		}
	}
}

func TestEveryPortalRouteRefusesAnAdminSession(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	for _, pattern := range coveragePatterns(t, app) {
		if !strings.Contains(pattern, "/portal/") {
			continue
		}
		t.Run(pattern, func(t *testing.T) {
			// Login is public, but an admin cookie cannot replace user credentials.
			r := routeCall(owner, pattern)
			if r.Code != 401 && r.Code != 403 {
				t.Fatalf("admin cookie on portal: %d %s", r.Code, r.Body.String())
			}
		})
	}
}

func TestPortalSessionTablesRejectRelabelledCookies(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	u := portalUser(t, app, owner, "namespace_user")
	c := portalLogin(t, app, u.Username, portalPassword)
	portalAsAdmin := budgetHTTP{app: app, cookie: &http.Cookie{Name: owner.cookie.Name, Value: c.cookie.Value}, csrf: c.csrf}
	if r := portalAsAdmin.call("GET", "/api/v1/users", nil); r.Code != 401 {
		t.Fatalf("portal token resolved in admin table: %d", r.Code)
	}
	adminAsPortal := budgetHTTP{app: app, cookie: &http.Cookie{Name: c.cookie.Name, Value: owner.cookie.Value}, csrf: owner.csrf}
	if r := adminAsPortal.call("GET", "/api/v1/portal/me", nil); r.Code != 401 {
		t.Fatalf("admin token resolved in portal table: %d", r.Code)
	}
}

func TestPortalLimiterCountsBothIPAndUsername(t *testing.T) {
	app := testServer(t)
	call := func(ip, username string) int {
		r := httptest.NewRequest("POST", "/api/v1/portal/auth/login", strings.NewReader(`{"username":"`+username+`","password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		r.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		app.HTTP.Handler.ServeHTTP(w, r)
		return w.Code
	}
	for i, ip := range []string{"192.0.2.10", "192.0.2.11", "192.0.2.12", "192.0.2.13", "192.0.2.14"} {
		if code := call(ip, "same_user"); code != 401 {
			t.Fatalf("username attempt %d: %d", i, code)
		}
	}
	if code := call("192.0.2.15", "same_user"); code != 429 {
		t.Fatalf("username bucket bypassed by IP rotation: %d", code)
	}
	for _, name := range []string{"different_a", "different_b", "different_c", "different_d", "different_e"} {
		if code := call("192.0.2.20", name); code != 401 {
			t.Fatalf("IP attempt: %d", code)
		}
	}
	if code := call("192.0.2.20", "different_f"); code != 429 {
		t.Fatalf("IP bucket bypassed by username rotation: %d", code)
	}
}

func TestPortalRateLimitCSRFExpiryAndCleanup(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	u := portalUser(t, app, owner, "security_user")
	c := portalLogin(t, app, u.Username, portalPassword)
	if c.cookie.Name != "qui_portal_session" || !c.cookie.HttpOnly || c.cookie.SameSite != http.SameSiteStrictMode || c.cookie.Path != "/" {
		t.Fatalf("cookie: %+v", c.cookie)
	}
	var stored string
	if err := app.Database.QueryRow("SELECT token_hash FROM user_sessions WHERE user_id=?", u.ID).Scan(&stored); err != nil || stored != auth.HashToken(c.cookie.Value) {
		t.Fatalf("session token was not hashed: %v", err)
	}
	noCSRF := c
	noCSRF.csrf = ""
	if r := noCSRF.call("POST", "/api/v1/portal/auth/logout", nil); r.Code != 403 {
		t.Fatalf("missing CSRF: %d", r.Code)
	}
	for _, headers := range []map[string]string{{"Origin": "https://evil.example"}, {"Sec-Fetch-Site": "cross-site"}} {
		for _, path := range []string{"/api/v1/portal/auth/login", "/api/v1/portal/subscription/reset"} {
			req := httptest.NewRequest("POST", path, strings.NewReader(`{"username":"security_user","password":"`+portalPassword+`"}`))
			req.AddCookie(c.cookie)
			req.Header.Set("X-CSRF-Token", c.csrf)
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			r := httptest.NewRecorder()
			app.HTTP.Handler.ServeHTTP(r, req)
			if r.Code != 403 {
				t.Fatalf("cross origin %s: %d", path, r.Code)
			}
		}
	}
	if _, err := app.Database.Exec("UPDATE user_sessions SET expires_at=?", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if r := c.call("GET", "/api/v1/portal/me", nil); r.Code != 401 {
		t.Fatalf("expired session: %d", r.Code)
	}
	// An unpresented expired row is removed by the next login, like admins.
	if _, err := app.Database.Exec("INSERT INTO user_sessions VALUES(?,?,?,?)", auth.HashToken("never-presented"), u.ID, "2000-01-01T00:00:00.000000000Z", "2000-01-01T00:00:00.000000000Z"); err != nil {
		t.Fatal(err)
	}
	if r := owner.call("PUT", "/api/v1/system/runtime-policy", map[string]any{"expectedRevision": 0, "policy": map[string]any{"sessionMinutes": 15, "subscriptionFormat": "uri"}}); r.Code != 200 {
		t.Fatalf("runtime settings: %d %s", r.Code, r.Body.String())
	}
	c = portalLogin(t, app, u.Username, portalPassword)
	if d := time.Until(c.cookie.Expires); d < 14*time.Minute || d > 16*time.Minute {
		t.Fatalf("runtime lifetime ignored: %s", d)
	}
	var count int
	if err := app.Database.QueryRow("SELECT count(*) FROM user_sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("expired cleanup: %d %v", count, err)
	}
	bad := budgetHTTP{app: app}
	for i := 0; i < 5; i++ {
		if r := bad.call("POST", "/api/v1/portal/auth/login", map[string]string{"username": u.Username, "password": "wrong"}); r.Code != 401 {
			t.Fatalf("attempt %d: %d", i, r.Code)
		}
	}
	if r := bad.call("POST", "/api/v1/portal/auth/login", map[string]string{"username": u.Username, "password": portalPassword}); r.Code != 429 || r.Header().Get("Retry-After") != "900" {
		t.Fatalf("rate limit: %d", r.Code)
	}
	// The same shared limiter keeps admin and portal keys in separate buckets.
	if err := app.authService.ReserveLogin(context.Background(), "192.0.2.1", u.Username); err != nil {
		t.Fatalf("portal attempts polluted admin buckets: %v", err)
	}
	if r := c.call("POST", "/api/v1/portal/auth/logout", nil); r.Code != 204 {
		t.Fatalf("logout: %d", r.Code)
	}
	if r := c.call("GET", "/api/v1/portal/auth/session", nil); r.Code != 401 {
		t.Fatalf("logged out cookie: %d", r.Code)
	}
}

func TestPortalInactiveUsersCanLoginButCannotSubscribe(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	for _, reason := range []string{"disabled", "expired", "quota_exceeded"} {
		t.Run(reason, func(t *testing.T) {
			u := portalUser(t, app, owner, reason)
			r := owner.call("POST", "/api/v1/users/"+u.ID+"/subscription-token", map[string]any{})
			var link struct {
				Path string `json:"path"`
			}
			if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &link) != nil {
				t.Fatalf("token: %d %s", r.Code, r.Body.String())
			}
			query := "UPDATE users SET status='disabled' WHERE id=?"
			if reason == "expired" {
				query = "UPDATE users SET expires_at='2000-01-01T00:00:00Z' WHERE id=?"
			}
			if reason == "quota_exceeded" {
				query = "UPDATE users SET traffic_limit=10,used_traffic=10 WHERE id=?"
			}
			if _, err := app.Database.Exec(query, u.ID); err != nil {
				t.Fatal(err)
			}
			c := portalLogin(t, app, u.Username, portalPassword)
			r = c.call("GET", "/api/v1/portal/me", nil)
			var me map[string]any
			if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &me) != nil || me["reason"] != reason {
				t.Fatalf("reason: %d %s", r.Code, r.Body.String())
			}
			if value, ok := me["deviceLimit"]; !ok || value != float64(0) {
				t.Fatal("Phase 10 must expose the persisted unlimited default")
			}
			if value, ok := me["onlineDevices"]; !ok || value != nil {
				t.Fatal("unobserved onlineDevices must remain explicitly null")
			}
			if r := c.call("GET", link.Path, nil); r.Code != 403 {
				t.Fatalf("inactive subscription: %d", r.Code)
			}
		})
	}
}

func TestPortalOwnerChangesImmediatelyRevokeRequests(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	for _, action := range []string{"disable", "reset", "revoke", "delete"} {
		t.Run(action, func(t *testing.T) {
			u := portalUser(t, app, owner, "revoke_"+action)
			c := portalLogin(t, app, u.Username, portalPassword)
			var r *httptest.ResponseRecorder
			switch action {
			case "disable":
				r = owner.call("PUT", "/api/v1/users/"+u.ID+"/login", map[string]any{"enabled": false})
			case "reset":
				r = owner.call("PUT", "/api/v1/users/"+u.ID+"/login", map[string]any{"enabled": true, "password": "owner-reset-password-2"})
			case "revoke":
				r = owner.call("DELETE", "/api/v1/users/"+u.ID+"/sessions", nil)
			case "delete":
				r = owner.call("DELETE", "/api/v1/users/"+u.ID, nil)
			}
			if r.Code != 204 {
				t.Fatalf("owner change: %d %s", r.Code, r.Body.String())
			}
			if r := c.call("GET", "/api/v1/portal/me", nil); r.Code != 401 {
				t.Fatalf("stale cookie after %s: %d %s", action, r.Code, r.Body.String())
			}
		})
	}
}

func portalSubscriptions(t *testing.T, app *Server) *subscription.Repository {
	t.Helper()
	v, err := vault.Open(app.Config.NodeIdentityDirectory + "/configuration.key")
	if err != nil {
		t.Fatal(err)
	}
	return &subscription.Repository{Database: app.Database, Vault: v}
}

func TestPortalSubscriptionRecoveryAndReset(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	u := portalUser(t, app, owner, "link_user")
	c := portalLogin(t, app, u.Username, portalPassword)
	r := c.call("POST", "/api/v1/portal/subscription/reset", nil)
	var first struct {
		Path    string `json:"path"`
		Formats []any  `json:"formats"`
	}
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &first) != nil || len(first.Formats) != len(subscription.DefaultFormats().Info()) {
		t.Fatalf("reset: %d %s", r.Code, r.Body.String())
	}
	if r := c.call("GET", "/api/v1/portal/subscription", nil); !strings.Contains(r.Body.String(), first.Path) {
		t.Fatalf("link not viewable: %s", r.Body.String())
	}
	if _, err := app.Database.Exec("UPDATE subscription_tokens SET encrypted_token=NULL WHERE user_id=?", u.ID); err != nil {
		t.Fatal(err)
	}
	if r := c.call("GET", "/api/v1/portal/subscription", nil); r.Code != 200 || !strings.Contains(r.Body.String(), `"state":"not_recoverable"`) {
		t.Fatalf("old token state: %d %s", r.Code, r.Body.String())
	}
	r = c.call("POST", "/api/v1/portal/subscription/reset", nil)
	if r.Code != 201 || strings.Contains(r.Body.String(), first.Path) {
		t.Fatalf("reset reused token: %d %s", r.Code, r.Body.String())
	}
	if _, err := portalSubscriptions(t, app).Resolve(context.Background(), strings.TrimPrefix(first.Path, "/sub/")); err == nil {
		t.Fatal("old token still resolves")
	}
	if r := c.call("GET", first.Path, nil); r.Code != 403 {
		t.Fatalf("old token HTTP: %d", r.Code)
	}
	anonymous := budgetHTTP{app: app}
	if r := anonymous.call("GET", "/api/v1/portal/subscription", nil); r.Code != 401 {
		t.Fatalf("anonymous link disclosure: %d", r.Code)
	}
}

func TestPortalEveryEndpointUsesOnlySessionUser(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	a := portalUser(t, app, owner, "private_alice")
	b := portalUser(t, app, owner, "private_bob")
	ac := portalLogin(t, app, a.Username, portalPassword)
	bc := portalLogin(t, app, b.Username, portalPassword)
	otherB := portalLogin(t, app, b.Username, portalPassword)
	for _, c := range []budgetHTTP{ac, bc} {
		if r := c.call("POST", "/api/v1/portal/subscription/reset", nil); r.Code != 201 {
			t.Fatal(r.Code)
		}
	}
	aToken, err := portalSubscriptions(t, app).Link(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Distinct granted nodes and traffic make an accidentally global read visible.
	for i, u := range []user.User{a, b} {
		n, _, _, err := app.nodeRepository.CreateEnrollment(context.Background(), u.Username+"_node", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := app.Database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES(?, 'portal entry',?,'http','0.0.0.0',1080,1,1,0,'direct','[]',x'01','now')", n.ID, n.ID); err != nil {
			t.Fatal(err)
		}
		g, err := app.nodeRepository.SaveGroup(context.Background(), "", node.GroupInput{Name: u.Username + "_group", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{n.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if err := (&user.Repository{Database: app.Database}).Grant(context.Background(), u.ID, g.ID, false); err != nil {
			t.Fatal(err)
		}
		for _, entry := range []struct{ table, bucket string }{{"account_traffic_daily", time.Now().UTC().Format("2006-01-02")}, {"account_traffic_hourly", time.Now().UTC().Truncate(time.Hour).Format("2006-01-02T15:04:05.000000000Z")}} {
			if _, err := app.Database.Exec("INSERT INTO "+entry.table+"(bucket,node_id,user_id,device_id,upload_bytes,download_bytes) VALUES(?,?,?,'',?,?)", entry.bucket, n.ID, u.ID, (i+1)*111, (i+1)*222); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, path := range []string{"/auth/session", "/me", "/nodes", "/subscription", "/traffic?range=7d", "/traffic?range=30d", "/traffic?range=24h"} {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		r := bc.call("GET", "/api/v1/portal"+path+sep+"userId="+a.ID+"&id="+a.ID, nil)
		if r.Code != 200 {
			t.Fatalf("%s: %d %s", path, r.Code, r.Body.String())
		}
		for _, secret := range []string{a.ID, a.Username, aToken} {
			if strings.Contains(r.Body.String(), secret) {
				t.Fatalf("%s leaked Alice data: %s", path, r.Body.String())
			}
		}
		if path == "/nodes" && !strings.Contains(r.Body.String(), b.Username+"_node") {
			t.Fatalf("Bob's granted node missing: %s", r.Body.String())
		}
		if (path == "/me" || path == "/auth/session") && !strings.Contains(r.Body.String(), b.ID) {
			t.Fatalf("Bob's profile missing: %s", r.Body.String())
		}
		if strings.HasPrefix(path, "/traffic") && (!strings.Contains(r.Body.String(), `"uploadBytes":222`) || strings.Contains(r.Body.String(), `"uploadBytes":111`)) {
			t.Fatalf("traffic not scoped: %s", r.Body.String())
		}
	}
	if r := bc.call("POST", "/api/v1/portal/subscription/reset?userId="+a.ID, map[string]string{"userId": a.ID}); r.Code != 201 {
		t.Fatalf("reset: %d", r.Code)
	}
	if token, err := portalSubscriptions(t, app).Link(context.Background(), a.ID); err != nil || token != aToken {
		t.Fatal("Bob reset Alice token")
	}
	var operator string
	if err := app.Database.QueryRow("SELECT operator FROM admin_operations WHERE action='重置订阅 Token' AND target=? ORDER BY timestamp DESC LIMIT 1", b.ID).Scan(&operator); err != nil || operator != b.Username {
		t.Fatalf("portal reset actor: %q %v", operator, err)
	}
	// Password ID injection is rejected by strict decoding; neither account changes.
	body := map[string]string{"currentPassword": portalPassword, "newPassword": "bob-new-password-2", "userId": a.ID}
	if r := bc.call("PUT", "/api/v1/portal/auth/password", body); r.Code != 400 {
		t.Fatalf("body ID accepted: %d", r.Code)
	}
	delete(body, "userId")
	if r := bc.call("PUT", "/api/v1/portal/auth/password?userId="+a.ID, body); r.Code != 204 {
		t.Fatalf("password: %d %s", r.Code, r.Body.String())
	}
	if r := otherB.call("GET", "/api/v1/portal/me", nil); r.Code != 401 {
		t.Fatalf("other Bob session survived: %d", r.Code)
	}
	if r := bc.call("GET", "/api/v1/portal/me", nil); r.Code != 200 {
		t.Fatal("password change ended current session")
	}
	if r := ac.call("GET", "/api/v1/portal/me", nil); r.Code != 200 {
		t.Fatal("Bob ended Alice session")
	}
	// Login rejects an ID field; valid credentials still select Bob only.
	if r := (budgetHTTP{app: app}).call("POST", "/api/v1/portal/auth/login", map[string]string{"username": b.Username, "password": "bob-new-password-2", "userId": a.ID}); r.Code != 400 {
		t.Fatalf("login ID injection: %d", r.Code)
	}
	_ = portalLogin(t, app, a.Username, portalPassword)
	_ = portalLogin(t, app, b.Username, "bob-new-password-2")
	if r := bc.call("POST", "/api/v1/portal/auth/logout?userId="+a.ID, map[string]string{"userId": a.ID}); r.Code != 204 {
		t.Fatal(r.Code)
	}
	if r := bc.call("GET", "/api/v1/portal/me", nil); r.Code != 401 {
		t.Fatal("Bob logout did not end Bob session")
	}
	if r := ac.call("GET", "/api/v1/portal/me", nil); r.Code != 200 {
		t.Fatal("Bob logout ended Alice session")
	}
}

func TestPortalMeExplainsWireGuardAndValidatesTrafficRange(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	u := portalUser(t, app, owner, "wg_user")
	if r := owner.call("PUT", "/api/v1/users/"+u.ID, map[string]any{"username": u.Username, "displayName": u.DisplayName, "status": "active", "allowedProtocols": []string{"wireguard"}}); r.Code != 200 {
		t.Fatalf("allow WireGuard: %d %s", r.Code, r.Body.String())
	}
	c := portalLogin(t, app, u.Username, portalPassword)
	r := c.call("GET", "/api/v1/portal/me", nil)
	var p portal.Profile
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &p) != nil || p.WireGuardNote == "" || p.Reason != "" {
		t.Fatalf("WireGuard profile: %d %s", r.Code, r.Body.String())
	}
	if r := c.call("GET", "/api/v1/portal/traffic?range=all", nil); r.Code != 400 {
		t.Fatalf("invalid traffic range: %d", r.Code)
	}
}

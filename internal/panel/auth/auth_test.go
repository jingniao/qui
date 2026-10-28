package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/db"
)

const testPassword = "test-password-unique-123"
const testSetupToken = "test-setup-token"

func installSetupToken(t *testing.T, service *auth.Service) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "setup-token")
	if err := os.WriteFile(path, []byte(testSetupToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service.SetSetupToken(testSetupToken, path)
	return path
}

func fixture(t *testing.T) (*auth.Service, *http.ServeMux) {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	settings := &system.Repository{Database: database}
	service := &auth.Service{Repository: &auth.Repository{Database: database}, Audit: settings, Settings: settings, SessionDuration: 2 * time.Hour}
	installSetupToken(t, service)
	handler := &auth.Handler{Service: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	handler.Register(mux)
	return service, mux
}

func send(t *testing.T, mux http.Handler, method, path string, body any, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	request.RemoteAddr = "192.0.2.10:4000"
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func mustStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("got %d, want %d: %s", response.Code, status, response.Body.String())
	}
}
func readSession(t *testing.T, response *httptest.ResponseRecorder) auth.Session {
	t.Helper()
	var session auth.Session
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	return session
}
func setup(t *testing.T, mux http.Handler) (*http.Cookie, auth.Session) {
	t.Helper()
	response := send(t, mux, "POST", "/api/v1/auth/setup", map[string]string{"token": testSetupToken, "username": "owner", "password": testPassword, "platformName": "Test Control", "timezone": "Asia/Tokyo"}, nil, "", "")
	mustStatus(t, response, 201)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	return cookies[0], readSession(t, response)
}

func TestAuthenticationPermissionsAndCredentialRotation(t *testing.T) {
	service, mux := fixture(t)
	response := send(t, mux, "GET", "/api/v1/auth/session", nil, nil, "", "")
	mustStatus(t, response, 200)
	if readSession(t, response).IsInitialized {
		t.Fatal("empty database already initialized")
	}
	mustStatus(t, send(t, mux, "GET", "/api/v1/admins", nil, nil, "", ""), 401)
	mustStatus(t, send(t, mux, "POST", "/api/v1/auth/setup", map[string]string{"token": testSetupToken, "username": "owner", "password": testPassword, "platformName": "Test", "timezone": "unknown/timezone"}, nil, "", ""), 400)
	initialized, err := service.Repository.Initialized(context.Background())
	if err != nil || initialized {
		t.Fatal("failed setup was not rolled back")
	}
	ownerCookie, owner := setup(t, mux)
	if !ownerCookie.HttpOnly || ownerCookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie protection missing")
	}
	if owner.Timezone != "Asia/Tokyo" || owner.PlatformName != "Test Control" {
		t.Fatal("setup preferences missing")
	}
	mustStatus(t, send(t, mux, "POST", "/api/v1/auth/setup", map[string]string{}, nil, "", ""), 409)
	var hash string
	if err := service.Repository.Database.QueryRow("SELECT password_hash FROM admins WHERE username='owner'").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == testPassword || !auth.VerifyPassword(hash, testPassword) {
		t.Fatal("password hashing failed")
	}
	var storedToken string
	if err := service.Repository.Database.QueryRow("SELECT token_hash FROM sessions").Scan(&storedToken); err != nil {
		t.Fatal(err)
	}
	if storedToken == ownerCookie.Value || storedToken != auth.HashToken(ownerCookie.Value) {
		t.Fatal("raw session token persisted")
	}
	viewerBody := map[string]string{"username": "viewer", "displayName": "Observer", "password": testPassword, "role": "Viewer"}
	mustStatus(t, send(t, mux, "POST", "/api/v1/admins", viewerBody, ownerCookie, "", ""), 403)
	mustStatus(t, send(t, mux, "POST", "/api/v1/admins", viewerBody, ownerCookie, owner.CSRFToken, "https://evil.example"), 403)
	response = send(t, mux, "POST", "/api/v1/admins", viewerBody, ownerCookie, owner.CSRFToken, "")
	mustStatus(t, response, 201)
	var viewer auth.Admin
	if err := json.Unmarshal(response.Body.Bytes(), &viewer); err != nil {
		t.Fatal(err)
	}
	if viewer.PasswordHash != "" || strings.Contains(response.Body.String(), "pbkdf2") {
		t.Fatal("API leaked credential hash")
	}
	response = send(t, mux, "GET", "/api/v1/admins?pageSize=1&page=2", nil, ownerCookie, "", "")
	mustStatus(t, response, 200)
	var page struct {
		Items []auth.Admin
		Total int
	}
	json.Unmarshal(response.Body.Bytes(), &page)
	if page.Total != 2 || len(page.Items) != 1 {
		t.Fatal("server pagination failed")
	}
	login := send(t, mux, "POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": testPassword}, nil, "", "")
	mustStatus(t, login, 200)
	viewerCookie := login.Result().Cookies()[0]
	viewerSession := readSession(t, login)
	mustStatus(t, send(t, mux, "GET", "/api/v1/admins", nil, viewerCookie, "", ""), 403)
	mustStatus(t, send(t, mux, "POST", "/api/v1/admins", viewerBody, viewerCookie, viewerSession.CSRFToken, ""), 403)
	mustStatus(t, send(t, mux, "PUT", "/api/v1/admins/"+owner.CurrentUser.ID, map[string]string{"displayName": "owner", "status": "disabled"}, ownerCookie, owner.CSRFToken, ""), 403)
	mustStatus(t, send(t, mux, "DELETE", "/api/v1/admins/"+owner.CurrentUser.ID, nil, ownerCookie, owner.CSRFToken, ""), 403)
	mustStatus(t, send(t, mux, "POST", "/api/v1/admins/"+viewer.ID+"/password", map[string]string{"password": "changed-viewer-password"}, ownerCookie, owner.CSRFToken, ""), 204)
	if readSession(t, send(t, mux, "GET", "/api/v1/auth/session", nil, viewerCookie, "", "")).IsAuthenticated {
		t.Fatal("reset did not revoke session")
	}
	newPassword := "new-owner-password-456"
	mustStatus(t, send(t, mux, "PUT", "/api/v1/auth/password", map[string]string{"currentPassword": "wrong", "newPassword": newPassword}, ownerCookie, owner.CSRFToken, ""), 401)
	mustStatus(t, send(t, mux, "PUT", "/api/v1/auth/password", map[string]string{"currentPassword": testPassword, "newPassword": newPassword}, ownerCookie, owner.CSRFToken, ""), 204)
	if readSession(t, send(t, mux, "GET", "/api/v1/auth/session", nil, ownerCookie, "", "")).IsAuthenticated {
		t.Fatal("password change did not revoke sessions")
	}
	mustStatus(t, send(t, mux, "POST", "/api/v1/auth/login", map[string]string{"username": "owner", "password": testPassword}, nil, "", ""), 401)
	response = send(t, mux, "POST", "/api/v1/auth/login", map[string]string{"username": "owner", "password": newPassword}, nil, "", "")
	mustStatus(t, response, 200)
	latest := readSession(t, response)
	cookie := response.Result().Cookies()[0]
	mustStatus(t, send(t, mux, "POST", "/api/v1/auth/logout", nil, cookie, latest.CSRFToken, ""), 204)
	mustStatus(t, send(t, mux, "GET", "/api/v1/admins", nil, cookie, "", ""), 401)
	operations, count, err := service.Audit.(*system.Repository).List(context.Background(), "", 1, 100)
	if err != nil || count < 7 {
		t.Fatalf("missing audit records: %d %v", count, err)
	}
	encoded, _ := json.Marshal(operations)
	for _, secret := range []string{testPassword, newPassword, ownerCookie.Value, owner.CSRFToken, hash} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("audit contains sensitive data")
		}
	}
}

func TestSessionAndRateLimitSurviveDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	database, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	settings := &system.Repository{Database: database}
	service := &auth.Service{Repository: &auth.Repository{Database: database}, Settings: settings, Audit: settings, SessionDuration: time.Hour}
	installSetupToken(t, service)
	token, _, err := service.Setup(ctx, testSetupToken, "owner", testPassword, "Test", "UTC", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, _, err := service.Login(ctx, "owner", "wrong-password", "192.0.2.2"); !errors.Is(err, auth.ErrCredentials) {
			t.Fatal(err)
		}
	}
	database.Close()
	database, err = db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	settings.Database = database
	service.Repository.Database = database
	session, err := service.Session(ctx, token)
	if err != nil || !session.IsAuthenticated {
		t.Fatalf("persisted session lost: %v", err)
	}
	if _, _, err := service.Login(ctx, "owner", testPassword, "192.0.2.2"); !errors.Is(err, auth.ErrRateLimited) {
		t.Fatalf("login lock lost: %v", err)
	}
	if _, err := database.Exec("UPDATE sessions SET expires_at=?", time.Now().UTC().Add(-time.Second).Format("2006-01-02T15:04:05.000000000Z")); err != nil {
		t.Fatal(err)
	}
	session, err = service.Session(ctx, token)
	if err != nil || session.IsAuthenticated {
		t.Fatal("expired session accepted")
	}
}

func TestInputValidationAndSecureCookie(t *testing.T) {
	service, mux := fixture(t)
	for _, password := range []string{"short", strings.Repeat("x", 129)} {
		if _, _, err := service.Setup(context.Background(), testSetupToken, "owner", password, "Test", "UTC", "192.0.2.1"); err == nil {
			t.Fatal("accepted invalid password")
		}
	}
	request := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"owner","password":"test","role":"Owner"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	mustStatus(t, response, 400)
	handler := &auth.Handler{Secure: true}
	response = httptest.NewRecorder()
	auth.SetCookie(handler, response, "opaque", time.Now().Add(time.Hour))
	cookie := response.Result().Cookies()[0]
	if cookie.Name != "__Host-proxy_session" || !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/" {
		t.Fatal("insecure production cookie")
	}
	if auth.VerifyPassword("corrupt", testPassword) {
		t.Fatal("invalid hash accepted")
	}
}

func TestSetupRequiresOneTimeToken(t *testing.T) {
	service, mux := fixture(t)
	tokenPath := service.CurrentSetupTokenPath()
	body := func(token string) map[string]string {
		return map[string]string{"token": token, "username": "owner", "password": testPassword, "platformName": "Test", "timezone": "UTC"}
	}
	mustStatus(t, send(t, mux, "POST", "/api/v1/auth/setup", body(""), nil, "", ""), 403)
	mustStatus(t, send(t, mux, "POST", "/api/v1/auth/setup", body("wrong-token"), nil, "", ""), 403)
	if initialized, err := service.Repository.Initialized(context.Background()); err != nil || initialized {
		t.Fatal("rejected setup created an Owner")
	}
	response := send(t, mux, "POST", "/api/v1/auth/setup", body(testSetupToken), nil, "", "")
	mustStatus(t, response, 201)
	if service.CurrentSetupToken() != "" {
		t.Fatal("setup token survived a successful setup")
	}
	if _, err := os.Stat(tokenPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("setup token file survived a successful setup")
	}
	mustStatus(t, send(t, mux, "POST", "/api/v1/auth/setup", body(testSetupToken), nil, "", ""), 409)
}

func TestConcurrentSetupWithCorrectTokenCreatesOneOwner(t *testing.T) {
	service, mux := fixture(t)
	payload, err := json.Marshal(map[string]string{"token": testSetupToken, "username": "owner", "password": testPassword, "platformName": "Test", "timezone": "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	codes := make(chan int, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			request := httptest.NewRequest("POST", "/api/v1/auth/setup", strings.NewReader(string(payload)))
			request.RemoteAddr = "192.0.2.10:4000"
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			codes <- response.Code
		}()
	}
	wait.Wait()
	close(codes)
	created := 0
	for code := range codes {
		if code == 201 {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("got %d successful concurrent setups, want exactly 1", created)
	}
	if initialized, err := service.Repository.Initialized(context.Background()); err != nil || !initialized {
		t.Fatalf("expected exactly one Owner: initialized=%v err=%v", initialized, err)
	}
}

func parseNetworks(t *testing.T, values ...string) []*net.IPNet {
	t.Helper()
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			t.Fatal(err)
		}
		networks = append(networks, network)
	}
	return networks
}

func TestLoginLimiterCountsIPAndUsername(t *testing.T) {
	service, _ := fixture(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := service.ReserveLogin(ctx, "203.0.113.7", "user-"+strconv.Itoa(i)); err != nil {
			t.Fatalf("attempt %d blocked early: %v", i, err)
		}
	}
	if err := service.ReserveLogin(ctx, "203.0.113.7", "user-5"); !errors.Is(err, auth.ErrRateLimited) {
		t.Fatalf("IP bucket not enforced: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := service.ReserveLogin(ctx, "198.51.100."+strconv.Itoa(i), "same-user"); err != nil {
			t.Fatalf("attempt %d blocked early: %v", i, err)
		}
	}
	if err := service.ReserveLogin(ctx, "198.51.100.9", "same-user"); !errors.Is(err, auth.ErrRateLimited) {
		t.Fatalf("username bucket not enforced: %v", err)
	}
	service.ClearLoginAttempts(ctx, "198.51.100.9", "same-user")
	if err := service.ReserveLogin(ctx, "198.51.100.9", "same-user"); err != nil {
		t.Fatalf("cleared buckets still blocked: %v", err)
	}
}

func TestLoginHonoursForwardedIPOnlyFromTrustedProxies(t *testing.T) {
	service, _ := fixture(t)
	handler := &auth.Handler{Service: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), TrustedProxies: parseNetworks(t, "10.0.0.0/8")}
	mux := http.NewServeMux()
	handler.Register(mux)
	login := func(remote, forwarded, username string) int {
		body, _ := json.Marshal(map[string]string{"username": username, "password": "wrong-password"})
		request := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(string(body)))
		request.RemoteAddr = remote
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-For", forwarded)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response.Code
	}
	for i := 0; i < 5; i++ {
		if code := login("10.1.2.3:1234", "203.0.113.50", "trusted-"+strconv.Itoa(i)); code != 401 {
			t.Fatalf("attempt %d: got %d", i, code)
		}
	}
	if code := login("10.1.2.3:1234", "203.0.113.50", "trusted-5"); code != 429 {
		t.Fatalf("forwarded IP from a trusted peer was not throttled: %d", code)
	}
	if code := login("10.1.2.3:1234", "203.0.113.60", "other"); code != 401 {
		t.Fatalf("an unrelated forwarded IP was throttled: %d", code)
	}
	for i := 0; i < 5; i++ {
		if code := login("192.0.2.99:1234", "203.0.113."+strconv.Itoa(70+i), "untrusted-"+strconv.Itoa(i)); code != 401 {
			t.Fatalf("attempt %d: got %d", i, code)
		}
	}
	if code := login("192.0.2.99:1234", "203.0.113.75", "untrusted-5"); code != 429 {
		t.Fatalf("forwarded IP from an untrusted peer bypassed the peer throttle: %d", code)
	}
}

func TestRequestIPTrustsOnlyRightmostUntrustedForwardedEntry(t *testing.T) {
	handler := &auth.Handler{TrustedProxies: parseNetworks(t, "10.0.0.0/8")}
	resolve := func(remote, forwarded, real string) string {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = remote
		if forwarded != "" {
			request.Header.Set("X-Forwarded-For", forwarded)
		}
		if real != "" {
			request.Header.Set("X-Real-IP", real)
		}
		return auth.RequestIP(handler, request)
	}
	cases := []struct {
		name      string
		remote    string
		forwarded string
		real      string
		want      string
	}{
		{"client-supplied leftmost entry is ignored", "10.1.2.3:1234", "6.6.6.6, 203.0.113.9", "", "203.0.113.9"},
		{"trailing trusted proxy entry is skipped", "10.1.2.3:1234", "203.0.113.9, 10.0.0.2", "", "203.0.113.9"},
		{"all forwarded entries trusted falls back to x-real-ip", "10.1.2.3:1234", "10.0.0.9, 10.0.0.8", "198.51.100.5", "198.51.100.5"},
		{"trusted entries and invalid x-real-ip fall back to peer", "10.1.2.3:1234", "10.0.0.9", "not-an-ip", "10.1.2.3"},
		{"invalid forwarded entries fall back to peer", "10.1.2.3:1234", "not-an-ip, still-not-an-ip", "", "10.1.2.3"},
		{"untrusted peer ignores forwarding", "192.0.2.7:1234", "203.0.113.9", "198.51.100.5", "192.0.2.7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(tc.remote, tc.forwarded, tc.real); got != tc.want {
				t.Fatalf("requestIP(%q, %q, %q) = %q, want %q", tc.remote, tc.forwarded, tc.real, got, tc.want)
			}
		})
	}
}

func TestLoginRateLimitIgnoresRotatedForwardedEntries(t *testing.T) {
	service, _ := fixture(t)
	handler := &auth.Handler{Service: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), TrustedProxies: parseNetworks(t, "10.0.0.0/8")}
	mux := http.NewServeMux()
	handler.Register(mux)
	login := func(forwarded, username string) int {
		body, _ := json.Marshal(map[string]string{"username": username, "password": "wrong-password"})
		request := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(string(body)))
		request.RemoteAddr = "10.1.2.3:1234"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-For", forwarded)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response.Code
	}
	for i := 0; i < 5; i++ {
		// The attacker rotates the leftmost entry it supplies; the trusted proxy
		// appends the real client address on the right.
		forwarded := strconv.Itoa(i) + ".6.6.6, 203.0.113.9"
		if code := login(forwarded, "rotate-"+strconv.Itoa(i)); code != 401 {
			t.Fatalf("attempt %d: got %d", i, code)
		}
	}
	if code := login("9.9.9.9, 203.0.113.9", "rotate-5"); code != 429 {
		t.Fatalf("rotating the client-supplied entry bypassed the per-IP limit: %d", code)
	}
}

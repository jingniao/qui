package notification

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
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil/dbtest"
)

type setupSettings struct{}

func (setupSettings) SaveSetup(ctx context.Context, tx *sql.Tx, name, timezone string) error {
	for key, value := range map[string]string{"platform_name": name, "timezone": timezone} {
		data, _ := json.Marshal(value)
		if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?)", key, string(data)); err != nil {
			return err
		}
	}
	return nil
}

func (setupSettings) ReadSetup(context.Context) (string, string, error) {
	return "Notification Panel", "UTC", nil
}

type fakeAuditor struct{}

func (fakeAuditor) Record(ctx context.Context, actor, action, target, result, ip string) error {
	return nil
}

type notificationHTTP struct {
	handler    http.Handler
	repository *Repository
	worker     *Worker
	database   *sql.DB
	owner      string
	ownerCSRF  string
	viewer     string
	viewerCSRF string
}

func newNotificationHTTP(t *testing.T) *notificationHTTP {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	database, err := dbtest.Open(ctx, filepath.Join(root, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	key, err := vault.Open(filepath.Join(root, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}

	repo := &Repository{Database: database, Vault: key}
	audit := &fakeAuditor{}
	authService := &auth.Service{
		Repository:      &auth.Repository{Database: database},
		Audit:           audit,
		Settings:        setupSettings{},
		SessionDuration: 2 * time.Hour,
	}

	const token, password = "notification-setup-token", "notif-password-123"
	tokenPath := filepath.Join(t.TempDir(), "setup-token")
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	authService.SetSetupToken(token, tokenPath)
	ownerToken, ownerSession, err := authService.Setup(ctx, token, "owner", password, "Notification Panel", "UTC", "192.0.2.10")
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

	worker := &Worker{
		Repository: repo,
		Sender:     Sender{},
	}

	handler := &Handler{
		Repository: repo,
		Worker:     worker,
		Auth: &auth.Handler{
			Service: authService,
			Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		Audit: audit,
	}

	mux := http.NewServeMux()
	handler.Register(mux)

	return &notificationHTTP{
		handler:    mux,
		repository: repo,
		worker:     worker,
		database:   database,
		owner:      ownerToken,
		ownerCSRF:  ownerSession.CSRFToken,
		viewer:     viewerToken,
		viewerCSRF: viewerSession.CSRFToken,
	}
}

func (h *notificationHTTP) call(t *testing.T, method, path, body, cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.10:4000"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "proxy_session", Value: cookie})
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func TestHandlerPermissionsMatrix(t *testing.T) {
	app := newNotificationHTTP(t)

	// 1. Viewer reading rules -> 403 Forbidden
	resp := app.call(t, http.MethodGet, "/api/v1/notification-rules", "", app.viewer, "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer reading rules, got %d", resp.Code)
	}

	// 2. Unauthenticated reading rules -> 401 Unauthorized
	resp = app.call(t, http.MethodGet, "/api/v1/notification-rules", "", "", "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated reading rules, got %d", resp.Code)
	}

	// 3. Owner reading rules -> 200 OK
	resp = app.call(t, http.MethodGet, "/api/v1/notification-rules", "", app.owner, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for owner reading rules, got %d", resp.Code)
	}
	var rulesResp struct {
		Items []Rule `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &rulesResp); err != nil {
		t.Fatal(err)
	}
	if len(rulesResp.Items) != 7 {
		t.Fatalf("expected 7 default rules, got %d", len(rulesResp.Items))
	}

	// 4. Viewer reading notifications -> 200 OK (only allowed 4 kinds)
	ctx := context.Background()
	_, err := app.repository.Insert(ctx, Notification{Kind: "cpu", Severity: "warning", Title: "CPU alert", Body: "cpu high"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.repository.Insert(ctx, Notification{Kind: "system_alert", Severity: "warning", Title: "System alert", Body: "sys alert"})
	if err != nil {
		t.Fatal(err)
	}

	resp = app.call(t, http.MethodGet, "/api/v1/notifications", "", app.viewer, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for viewer reading notifications, got %d", resp.Code)
	}
	var notifResp struct {
		Items  []Notification `json:"items"`
		Total  int            `json:"total"`
		Unread int            `json:"unread"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &notifResp); err != nil {
		t.Fatal(err)
	}
	if notifResp.Total != 1 || len(notifResp.Items) != 1 || notifResp.Items[0].Kind != "cpu" {
		t.Fatalf("viewer should only see node_offline, cpu, memory, disk; got %+v", notifResp)
	}
}

func TestHandlerPutRulesValidation(t *testing.T) {
	app := newNotificationHTTP(t)

	// Threshold out of bounds (150 > 100) -> 400 invalid_request
	body := `{"items":[{"kind":"cpu","enabled":true,"threshold":150,"durationMinutes":5,"cooldownMinutes":15}]}`
	resp := app.call(t, http.MethodPut, "/api/v1/notification-rules", body, app.owner, app.ownerCSRF)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out of bounds threshold, got %d", resp.Code)
	}
}

func TestHandlerChannelsListingHidesToken(t *testing.T) {
	app := newNotificationHTTP(t)

	// Create a telegram channel
	body := `{"kind":"telegram","name":"Ops Bot","enabled":true,"config":{"botToken":"123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11","chatId":"-10012345"}}`
	resp := app.call(t, http.MethodPost, "/api/v1/notification-channels", body, app.owner, app.ownerCSRF)
	if resp.Code != http.StatusOK && resp.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 creating channel, got %d: %s", resp.Code, resp.Body.String())
	}

	// List channels
	resp = app.call(t, http.MethodGet, "/api/v1/notification-channels", "", app.owner, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 listing channels, got %d", resp.Code)
	}
	raw := resp.Body.String()
	if strings.Contains(raw, "ABC-DEF1234ghIkl") {
		t.Fatalf("token leaked in channel listing: %s", raw)
	}

	var channelsResp struct {
		Items []Channel `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &channelsResp); err != nil {
		t.Fatal(err)
	}
	if len(channelsResp.Items) != 1 || channelsResp.Items[0].Last4 != "ew11" {
		t.Fatalf("unexpected channels response: %+v", channelsResp)
	}
}

func TestHandlerTestChannelReturnsFixedCode(t *testing.T) {
	app := newNotificationHTTP(t)

	// Testing non-existent channel -> 200 with code "channel_not_found"
	resp := app.call(t, http.MethodPost, "/api/v1/notification-channels/nonexistent/test", "", app.owner, app.ownerCSRF)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for test channel, got %d", resp.Code)
	}
	var testResp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &testResp); err != nil {
		t.Fatal(err)
	}
	if testResp.Code != "channel_not_found" {
		t.Fatalf("expected channel_not_found, got %q", testResp.Code)
	}
}

func TestHandlerWebhookConfigMerge(t *testing.T) {
	app := newNotificationHTTP(t)
	resp := app.call(t, "POST", "/api/v1/notification-channels", `{"kind":"webhook","name":"Ops","enabled":true,"config":{"url":"https://example.com/hook?token=abcd","format":"dingtalk","secret":"private-signing-key"}}`, app.owner, app.ownerCSRF)
	if resp.Code != 201 {
		t.Fatalf("create: %d %s", resp.Code, resp.Body.String())
	}
	var ch Channel
	if err := json.Unmarshal(resp.Body.Bytes(), &ch); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ body, format, secret string }{
		{`{"name":"Ops","enabled":true}`, "dingtalk", "private-signing-key"},
		{`{"name":"Ops","enabled":true,"config":{"url":"","format":"dingtalk","secret":""}}`, "dingtalk", "private-signing-key"},
		{`{"name":"Ops","enabled":true,"config":{"format":"dingtalk","clearSecret":true}}`, "dingtalk", ""},
		{`{"name":"Ops","enabled":true,"config":{"format":"feishu","secret":"new-key"}}`, "feishu", "new-key"},
		{`{"name":"Ops","enabled":true,"config":{"format":"wecom"}}`, "wecom", ""},
		{`{"name":"Ops","enabled":true,"config":{"format":"feishu","secret":"another-key"}}`, "feishu", "another-key"},
		{`{"name":"Ops","enabled":true,"config":{"format":"generic"}}`, "generic", ""},
		{`{"name":"Ops","enabled":true,"config":{"format":"dingtalk","secret":"discarded-key","clearSecret":true}}`, "dingtalk", ""},
	} {
		resp = app.call(t, "PUT", "/api/v1/notification-channels/"+ch.ID, tc.body, app.owner, app.ownerCSRF)
		if resp.Code != 200 {
			t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
		}
		cfg, err := app.repository.ChannelConfig(context.Background(), ch.ID)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(cfg)
		var fields map[string]string
		json.Unmarshal(raw, &fields)
		if cfg.URL != "https://example.com/hook?token=abcd" || fields["secret"] != tc.secret || fields["format"] != tc.format {
			t.Fatalf("incorrect merged config: %s", raw)
		}
		if strings.Contains(resp.Body.String(), "secret") || strings.Contains(resp.Body.String(), "token=") {
			t.Fatal("private config leaked")
		}
		listed := app.call(t, "GET", "/api/v1/notification-channels", "", app.owner, "")
		var listing struct {
			Items []map[string]any `json:"items"`
		}
		json.Unmarshal(listed.Body.Bytes(), &listing)
		if len(listing.Items) != 1 || listing.Items[0]["format"] != tc.format || listing.Items[0]["last4"] != "abcd" {
			t.Fatalf("listing: %s", listed.Body.String())
		}
	}
	for _, cfg := range []string{`{"url":""}`, `{"format":""}`, `{"format":"bogus"}`, `{"format":"generic","secret":"bad"}`, `{"format":"feishu","secret":"` + strings.Repeat("x", 1025) + `"}`, `{"format":"feishu","unknown":true}`} {
		resp = app.call(t, "PUT", "/api/v1/notification-channels/"+ch.ID, `{"name":"Ops","enabled":true,"config":`+cfg+`}`, app.owner, app.ownerCSRF)
		if resp.Code != 400 {
			t.Fatalf("accepted invalid update %s: %d", cfg, resp.Code)
		}
	}
	resp = app.call(t, "PUT", "/api/v1/notification-channels/"+ch.ID, `{"name":"Ops","enabled":true,"config":{"url":"https://example.com/new?token=wxyz","format":"feishu","secret":"replacement-key"}}`, app.owner, app.ownerCSRF)
	if resp.Code != 200 {
		t.Fatalf("replace URL: %d", resp.Code)
	}
	cfg, err := app.repository.ChannelConfig(context.Background(), ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://example.com/new?token=wxyz" || cfg.Secret != "replacement-key" || cfg.Format != "feishu" {
		t.Fatal("URL or secret replacement was not saved")
	}
	if strings.Contains(resp.Body.String(), "replacement-key") || strings.Contains(resp.Body.String(), "token=") {
		t.Fatal("replacement config leaked")
	}
}

func TestHandlerChannelFormatValidation(t *testing.T) {
	app := newNotificationHTTP(t)
	for _, tc := range []struct {
		kind, cfg string
		status    int
	}{
		{"webhook", `{"url":"https://example.com"}`, 201},
		{"webhook", `{"url":"https://example.com","format":"wecom"}`, 201},
		{"webhook", `{"url":"https://example.com","format":"unknown"}`, 400},
		{"webhook", `{"url":"https://example.com","secret":"bad"}`, 400},
		{"webhook", `{"url":"https://example.com","format":"wecom","secret":"bad"}`, 400},
		{"telegram", `{"botToken":"bot","chatId":"chat","format":"generic"}`, 400},
		{"telegram", `{"botToken":"bot","chatId":"chat","secret":"bad"}`, 400},
		{"webhook", `{"url":"https://example.com","format":"feishu","typo":true}`, 400},
		{"webhook", `{"url":"https://example.com","clearSecret":true}`, 400},
	} {
		resp := app.call(t, "POST", "/api/v1/notification-channels", `{"kind":"`+tc.kind+`","name":"Ops","config":`+tc.cfg+`}`, app.owner, app.ownerCSRF)
		if resp.Code != tc.status {
			t.Fatalf("%s: %d want %d", tc.cfg, resp.Code, tc.status)
		}
	}
}

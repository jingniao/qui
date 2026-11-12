package policy_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/internal/platform/vault"
)

type auditRecorder struct {
	actions []string
}

func (a *auditRecorder) Record(_ context.Context, _, action, _, _, _ string) error {
	a.actions = append(a.actions, action)
	return nil
}

type setupStub struct{}

func (setupStub) SaveSetup(context.Context, *sql.Tx, string, string) error { return nil }
func (setupStub) ReadSetup(context.Context) (string, string, error) {
	return "Test Control", "UTC", nil
}

func sessionFor(t *testing.T, database *sql.DB, role string) (*http.Cookie, string) {
	t.Helper()
	token := "session-" + strings.ToLower(role)
	csrf := auth.CSRF(token)
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	adminID := "admin-" + strings.ToLower(role)
	if _, err := database.Exec("INSERT INTO admins(id,username,display_name,role,status,password_hash,created_at) VALUES(?,?,?,?,'active','unused',?)", adminID, role, role, role, stamp); err != nil {
		t.Fatal(err)
	}
	hash := auth.HashToken(token)
	if _, err := database.Exec("INSERT INTO sessions(token_hash,admin_id,expires_at,created_at) VALUES(?,?,?,?)", hash, adminID, time.Now().UTC().Add(time.Hour).Format("2006-01-02T15:04:05.000000000Z"), stamp); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "proxy_session", Value: token}, csrf
}

func TestRefreshSourceEndpoint(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := dbtest.Open(ctx, filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	v, err := vault.Open(filepath.Join(dir, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}

	audit := &auditRecorder{}
	repo := &policy.Repository{Database: database, Vault: v}
	service := &policy.Service{Repository: repo}

	fetcher := &fakeFetcher{
		fetchFunc: func(ctx context.Context, url string) ([]byte, error) {
			return []byte("refreshed.example.com\n"), nil
		},
	}
	refresher := &policy.SourceRefresher{
		Repository: repo,
		Fetcher:    fetcher,
		Vault:      v,
		Now:        time.Now,
	}

	authHandler := &auth.Handler{
		Service: &auth.Service{Repository: &auth.Repository{Database: database}, Settings: setupStub{}, Audit: audit, SessionDuration: time.Hour},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	handler := &policy.Handler{
		Service:   service,
		Refresher: refresher,
		Auth:      authHandler,
		Audit:     audit,
	}

	mux := http.NewServeMux()
	handler.Register(mux)

	// Create service set and source
	sources := []policy.SourceInput{
		{
			ID:              "src-refresh-1",
			URL:             "https://example.com/rules.txt",
			Format:          "text",
			Behavior:        "domain",
			IntervalSeconds: 86400,
		},
	}
	svc, err := repo.SaveService(ctx, "", policy.ServiceInput{
		Name:    "Refresh Test Svc",
		Enabled: true,
		Sources: &sources,
	})
	if err != nil {
		t.Fatal(err)
	}

	ownerCookie, ownerCSRF := sessionFor(t, database, "Owner")
	viewerCookie, viewerCSRF := sessionFor(t, database, "Viewer")

	refreshURL := "/api/v1/service-sets/" + svc.ID + "/sources/src-refresh-1/refresh"

	// 1. Viewer is rejected (403)
	req := httptest.NewRequest("POST", refreshURL, nil)
	req.AddCookie(viewerCookie)
	req.Header.Set("X-CSRF-Token", viewerCSRF)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer, got %d", rec.Code)
	}

	// 2. Owner without CSRF is rejected (403)
	req = httptest.NewRequest("POST", refreshURL, nil)
	req.AddCookie(ownerCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF, got %d", rec.Code)
	}

	// 3. Owner with CSRF succeeds (200)
	req = httptest.NewRequest("POST", refreshURL, nil)
	req.AddCookie(ownerCookie)
	req.Header.Set("X-CSRF-Token", ownerCSRF)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var res policy.ExternalSource
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.ID != "src-refresh-1" || res.LastStatus != "ok" || res.EntryCount != 1 {
		t.Fatalf("unexpected source response: %+v", res)
	}

	// Verify audit recorded
	foundAudit := false
	for _, act := range audit.actions {
		if act == "刷新规则集来源" {
			foundAudit = true
			break
		}
	}
	if !foundAudit {
		t.Errorf("expected audit action '刷新规则集来源', got: %v", audit.actions)
	}
}

package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
)

type mockCertLib struct {
	covering func(ctx context.Context, domain string) ([]LibraryMatch, error)
	cfConfig func(ctx context.Context) (bool, error)
}

func (m *mockCertLib) Covering(ctx context.Context, domain string) ([]LibraryMatch, error) {
	if m.covering != nil {
		return m.covering(ctx, domain)
	}
	return nil, nil
}

func (m *mockCertLib) CloudflareConfigured(ctx context.Context) (bool, error) {
	if m.cfConfig != nil {
		return m.cfConfig(ctx)
	}
	return false, nil
}

type certMatchesPanel struct {
	mux        *http.ServeMux
	repository *Repository
	database   *sql.DB
	handler    *Handler
}

func newCertMatchesPanel(t *testing.T, lib CertificateLibrary) *certMatchesPanel {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	service := &auth.Service{Repository: &auth.Repository{Database: database}, Settings: sessionSettings{}, SessionDuration: time.Hour}
	handler := &Handler{
		Repository:   &Repository{Database: database},
		Audit:        reissueAudit{testAudit{database: database}},
		Auth:         &auth.Handler{Service: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
		Certificates: lib,
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	return &certMatchesPanel{mux: mux, repository: handler.Repository, database: database, handler: handler}
}

func (p *certMatchesPanel) session(t *testing.T, role string) (*http.Cookie, string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	stamp := now.Format("2006-01-02T15:04:05.000000000Z")
	token := "session-" + role
	if _, err := p.database.ExecContext(ctx, "INSERT INTO admins(id,username,display_name,role,status,password_hash,created_at) VALUES(?,?,?,?,'active','unused',?)", role, role, role, role, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.ExecContext(ctx, "INSERT INTO sessions(token_hash,admin_id,expires_at,created_at) VALUES(?,?,?,?)", digest(token), role, now.Add(time.Hour).Format("2006-01-02T15:04:05.000000000Z"), stamp); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "proxy_session", Value: token}, digest("csrf:" + token)
}

func TestCoversDomainWildcardOneLabel(t *testing.T) {
	names := []string{"*.example.com"}
	if !coversDomain(names, "a.example.com") {
		t.Fatal("expected *.example.com to cover a.example.com")
	}
	if coversDomain(names, "a.b.example.com") {
		t.Fatal("expected *.example.com NOT to cover a.b.example.com")
	}
	if coversDomain(names, "example.com") {
		t.Fatal("expected *.example.com NOT to cover example.com")
	}
	if !coversDomain(names, "A.EXAMPLE.COM") {
		t.Fatal("expected case-insensitive match for A.EXAMPLE.COM")
	}
	if !coversDomain([]string{"example.com"}, "example.com") {
		t.Fatal("expected exact match for example.com")
	}
	if !coversDomain([]string{"192.168.1.1"}, "192.168.1.1") {
		t.Fatal("expected IP match for 192.168.1.1")
	}
	if coversDomain([]string{"192.168.1.1"}, "192.168.1.2") {
		t.Fatal("expected 192.168.1.1 NOT to cover 192.168.1.2")
	}
}

func TestMatchesFromInventoryAndLibrary(t *testing.T) {
	lib := &mockCertLib{
		covering: func(ctx context.Context, domain string) ([]LibraryMatch, error) {
			if domain == "a.example.com" {
				return []LibraryMatch{
					{ID: "c1", Domains: []string{"a.example.com"}, NotAfter: "2027-01-15T00:00:00Z"},
				}, nil
			}
			return nil, nil
		},
		cfConfig: func(ctx context.Context) (bool, error) {
			return true, nil
		},
	}
	p := newCertMatchesPanel(t, lib)
	cookie, _ := p.session(t, "Owner")

	ctx := context.Background()
	n, _, _, err := p.repository.CreateEnrollment(ctx, "TestNode")
	if err != nil {
		t.Fatal(err)
	}

	future := time.Now().Add(365 * 24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	caps := &pb.Capabilities{
		Host: &pb.HostInfo{
			CertificateInventorySupported: true,
			CertificateFiles: []*pb.CertificateFileSummary{
				{
					CertificatePath: "/etc/proxy-agent/certs/old.pem",
					KeyPath:         "/etc/proxy-agent/certs/old.key",
					DnsNames:        []string{"a.example.com"},
					NotAfter:        timestamppb.New(past),
				},
				{
					CertificatePath: "/etc/proxy-agent/certs/fullchain.pem",
					KeyPath:         "/etc/proxy-agent/certs/privkey.pem",
					DnsNames:        []string{"a.example.com"},
					NotAfter:        timestamppb.New(future),
				},
			},
		},
	}
	data, err := protojson.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=? WHERE id=?", string(data), n.ID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+n.ID+"/certificate-matches?domain=a.example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	p.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp CertificateMatches
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Domain != "a.example.com" {
		t.Errorf("expected domain a.example.com, got %s", resp.Domain)
	}
	if !resp.InventorySupported {
		t.Error("expected InventorySupported=true")
	}
	if !resp.CloudflareConfigured {
		t.Error("expected CloudflareConfigured=true")
	}
	if len(resp.Library) != 1 || resp.Library[0].ID != "c1" {
		t.Errorf("unexpected library matches: %+v", resp.Library)
	}
	if len(resp.NodeFiles) != 2 {
		t.Fatalf("expected 2 nodeFiles, got %d", len(resp.NodeFiles))
	}
	// Check sort order: unexpired first, then expired
	if resp.NodeFiles[0].Expired {
		t.Error("expected unexpired file first")
	}
	if !resp.NodeFiles[1].Expired {
		t.Error("expected expired file second")
	}
	if resp.NodeFiles[0].CertificatePath != "/etc/proxy-agent/certs/fullchain.pem" {
		t.Errorf("expected fullchain.pem first, got %s", resp.NodeFiles[0].CertificatePath)
	}
}

func TestMatchesWithoutInventory(t *testing.T) {
	p := newCertMatchesPanel(t, &mockCertLib{})
	cookie, _ := p.session(t, "Owner")

	ctx := context.Background()
	n, _, _, err := p.repository.CreateEnrollment(ctx, "TestNodeOldAgent")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+n.ID+"/certificate-matches?domain=a.example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	p.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	var resp CertificateMatches
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.InventorySupported {
		t.Error("expected InventorySupported=false")
	}
	if resp.NodeFiles == nil || len(resp.NodeFiles) != 0 {
		t.Errorf("expected empty non-nil nodeFiles, got %+v", resp.NodeFiles)
	}
}

func TestMatchesSkipFilesWithoutKey(t *testing.T) {
	p := newCertMatchesPanel(t, &mockCertLib{})
	cookie, _ := p.session(t, "Owner")

	ctx := context.Background()
	n, _, _, err := p.repository.CreateEnrollment(ctx, "TestNode")
	if err != nil {
		t.Fatal(err)
	}

	caps := &pb.Capabilities{
		Host: &pb.HostInfo{
			CertificateInventorySupported: true,
			CertificateFiles: []*pb.CertificateFileSummary{
				{
					CertificatePath: "/etc/proxy-agent/certs/pubonly.pem",
					KeyPath:         "", // no key!
					DnsNames:        []string{"a.example.com"},
					NotAfter:        timestamppb.New(time.Now().Add(365 * 24 * time.Hour)),
				},
			},
		},
	}
	data, err := protojson.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=? WHERE id=?", string(data), n.ID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+n.ID+"/certificate-matches?domain=a.example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	p.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	var resp CertificateMatches
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.NodeFiles) != 0 {
		t.Errorf("expected files without key to be skipped, got %+v", resp.NodeFiles)
	}
}

func TestMatchesRejectsBadDomain(t *testing.T) {
	p := newCertMatchesPanel(t, &mockCertLib{})
	cookie, _ := p.session(t, "Owner")

	ctx := context.Background()
	n, _, _, err := p.repository.CreateEnrollment(ctx, "TestNode")
	if err != nil {
		t.Fatal(err)
	}

	badDomains := []string{"", "https://example.com", "example.com:443", "-bad.com", "bad domain"}
	for _, bad := range badDomains {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+n.ID+"/certificate-matches?domain="+url.QueryEscape(bad), nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		p.mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("domain %q expected 400 Bad Request, got %d", bad, w.Code)
		}
	}
}

func TestMatchesViewerForbidden(t *testing.T) {
	p := newCertMatchesPanel(t, &mockCertLib{})
	cookie, _ := p.session(t, "Viewer")

	ctx := context.Background()
	n, _, _, err := p.repository.CreateEnrollment(ctx, "TestNode")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+n.ID+"/certificate-matches?domain=a.example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	p.mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for Viewer, got %d", w.Code)
	}
}

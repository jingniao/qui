package deployment

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogueServesOnlyKnownPaths(t *testing.T) {
	dir := t.TempDir()

	// Setup legal manifest
	manifestDir := filepath.Join(dir, "proxy-agent", "linux", "amd64", "1005000")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(manifestDir, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"version":"1.5.0"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Setup legal artifact
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	artifactPath := filepath.Join(artifactDir, digest+".bin")
	if err := os.WriteFile(artifactPath, []byte("binary data"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := CatalogueHandler(dir)

	// Case 1: Legal manifest -> 200
	req := httptest.NewRequest("GET", "/agent-catalogue/proxy-agent/linux/amd64/1005000/manifest.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for legal manifest, got %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected Cache-Control: no-store, got %s", rec.Header().Get("Cache-Control"))
	}
	if rec.Body.String() != `{"version":"1.5.0"}` {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}

	// Case 2: Legal artifact -> 200
	req = httptest.NewRequest("GET", "/agent-catalogue/artifacts/"+digest+".bin", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for legal artifact, got %d", rec.Code)
	}

	// Case 3: ../config.yaml -> 404
	req = httptest.NewRequest("GET", "/agent-catalogue/../config.yaml", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for traversal, got %d", rec.Code)
	}

	// Case 4: Directory itself -> 404
	req = httptest.NewRequest("GET", "/agent-catalogue/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for directory, got %d", rec.Code)
	}

	// Case 5: artifacts/abc.bin (not 64 hex chars) -> 404
	req = httptest.NewRequest("GET", "/agent-catalogue/artifacts/abc.bin", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid artifact name, got %d", rec.Code)
	}

	// Case 6: Non-existent legal path -> 404
	req = httptest.NewRequest("GET", "/agent-catalogue/proxy-agent/linux/arm64/9999999/manifest.json", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent path, got %d", rec.Code)
	}
}

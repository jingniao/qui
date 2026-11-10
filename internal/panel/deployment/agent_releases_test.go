package deployment

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/pkg/contracts/release"
)

type fakeAgentReleaseStore struct {
	saved []release.CatalogueEntry
}

func (s *fakeAgentReleaseStore) SaveRelease(ctx context.Context, signed release.Signed, key ed25519.PublicKey, author, ip string) (release.CatalogueEntry, error) {
	for _, entry := range s.saved {
		if entry.Signed.Manifest == signed.Manifest {
			return entry, nil
		}
	}
	entry := release.CatalogueEntry{
		ID:        fmt.Sprintf("rel-%s-%s", signed.Manifest.Version, signed.Manifest.Architecture),
		Signed:    signed,
		CreatedBy: author,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.saved = append(s.saved, entry)
	return entry, nil
}

func generateSigningKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func createSignedAgentAsset(t *testing.T, version, arch string, seq int64, priv ed25519.PrivateKey, content []byte) (string, []byte) {
	t.Helper()
	sum := sha256.Sum256(content)
	manifest := release.Manifest{
		SchemaVersion: 1,
		Component:     "proxy-agent",
		Version:       version,
		OS:            "linux",
		Architecture:  arch,
		Sequence:      seq,
		Bytes:         int64(len(content)),
		SHA256:        hex.EncodeToString(sum[:]),
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(365 * 24 * time.Hour),
	}
	signed, err := release.Sign(manifest, priv)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(manifestBytes), content
}

func TestListAgentReleasesSkipsUnsignedAndPrerelease(t *testing.T) {
	fake := &fakeGitHub{
		lists: map[string]string{
			"jingniao/qui": `[
				{
					"tag_name": "v1.5.0-rc.1",
					"prerelease": true,
					"published_at": "2026-11-01T00:00:00Z",
					"assets": [
						{"name": "proxy-agent-1.5.0-rc.1-linux-amd64.bin"},
						{"name": "proxy-agent-1.5.0-rc.1-linux-amd64.manifest.json"}
					]
				},
				{
					"tag_name": "v1.4.0",
					"published_at": "2026-10-01T00:00:00Z",
					"assets": [
						{"name": "proxy-control-1.4.0-linux-amd64.tar.gz"}
					]
				},
				{
					"tag_name": "v1.4.6",
					"published_at": "2026-10-20T00:00:00Z",
					"assets": [
						{"name": "proxy-agent-1.4.6-linux-amd64.bin"},
						{"name": "proxy-agent-1.4.6-linux-amd64.manifest.json"},
						{"name": "proxy-agent-1.4.6-linux-arm64.bin"},
						{"name": "proxy-agent-1.4.6-linux-arm64.manifest.json"}
					]
				}
			]`,
		},
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	svc := &AgentReleases{
		Directory: t.TempDir(),
		Client:    server.Client(),
		API:       server.URL + "/repos/jingniao/qui",
	}

	list, err := svc.List(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 formal release with manifest, got %d", len(list))
	}
	if list[0].Version != "1.4.6" {
		t.Fatalf("expected version 1.4.6, got %s", list[0].Version)
	}
	if len(list[0].Architectures) != 2 {
		t.Fatalf("expected 2 architectures, got %v", list[0].Architectures)
	}
	if list[0].Imported {
		t.Fatalf("expected imported=false before import")
	}
}

func TestImportAgentReleaseWritesCatalogue(t *testing.T) {
	pub, priv := generateSigningKey(t)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	amdBin := []byte("amd64 agent binary content")
	armBin := []byte("arm64 agent binary content")
	amdManifest, _ := createSignedAgentAsset(t, "1.5.0", "amd64", 1005000, priv, amdBin)
	armManifest, _ := createSignedAgentAsset(t, "1.5.0", "arm64", 1005000, priv, armBin)

	fake := &fakeGitHub{
		lists: map[string]string{
			"jingniao/qui": `[
				{
					"tag_name": "v1.5.0",
					"published_at": "2026-11-09T00:00:00Z",
					"assets": [
						{"name": "proxy-agent-1.5.0-linux-amd64.bin", "browser_download_url": "/download/amd64.bin"},
						{"name": "proxy-agent-1.5.0-linux-amd64.manifest.json", "browser_download_url": "/download/amd64.json"},
						{"name": "proxy-agent-1.5.0-linux-arm64.bin", "browser_download_url": "/download/arm64.bin"},
						{"name": "proxy-agent-1.5.0-linux-arm64.manifest.json", "browser_download_url": "/download/arm64.json"}
					]
				}
			]`,
		},
		files: map[string]string{
			"/download/amd64.bin":  string(amdBin),
			"/download/amd64.json": amdManifest,
			"/download/arm64.bin":  string(armBin),
			"/download/arm64.json": armManifest,
		},
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	dir := t.TempDir()
	store := &fakeAgentReleaseStore{}
	svc := &AgentReleases{
		Directory: dir,
		Store:     store,
		Keys:      []release.KeyPin{{PublicKey: pubB64, Components: []string{"proxy-agent"}}},
		Client:    server.Client(),
		API:       server.URL + "/repos/jingniao/qui",
	}

	entries, err := svc.Import(context.Background(), "1.5.0", "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if len(store.saved) != 2 {
		t.Fatalf("expected SaveRelease to be called twice, got %d", len(store.saved))
	}

	// Verify catalogue files
	amdManifestPath := filepath.Join(dir, "proxy-agent", "linux", "amd64", "1005000", "manifest.json")
	if _, err := os.Stat(amdManifestPath); err != nil {
		t.Fatalf("amd64 manifest missing: %v", err)
	}
	armManifestPath := filepath.Join(dir, "proxy-agent", "linux", "arm64", "1005000", "manifest.json")
	if _, err := os.Stat(armManifestPath); err != nil {
		t.Fatalf("arm64 manifest missing: %v", err)
	}

	amdSum := sha256.Sum256(amdBin)
	amdArtifactPath := filepath.Join(dir, "artifacts", hex.EncodeToString(amdSum[:])+".bin")
	if _, err := os.Stat(amdArtifactPath); err != nil {
		t.Fatalf("amd64 artifact missing: %v", err)
	}

	// Now check that list reports imported=true
	list, err := svc.List(context.Background(), true)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 || !list[0].Imported {
		t.Fatalf("expected imported=true, got %+v", list)
	}
}

func TestImportAgentReleaseIsIdempotent(t *testing.T) {
	pub, priv := generateSigningKey(t)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	amdBin := []byte("amd64 agent binary")
	amdManifest, _ := createSignedAgentAsset(t, "1.5.0", "amd64", 1005000, priv, amdBin)

	fake := &fakeGitHub{
		lists: map[string]string{
			"jingniao/qui": `[
				{
					"tag_name": "v1.5.0",
					"published_at": "2026-11-09T00:00:00Z",
					"assets": [
						{"name": "proxy-agent-1.5.0-linux-amd64.bin", "browser_download_url": "/download/amd64.bin"},
						{"name": "proxy-agent-1.5.0-linux-amd64.manifest.json", "browser_download_url": "/download/amd64.json"}
					]
				}
			]`,
		},
		files: map[string]string{
			"/download/amd64.bin":  string(amdBin),
			"/download/amd64.json": amdManifest,
		},
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	dir := t.TempDir()
	store := &fakeAgentReleaseStore{}
	svc := &AgentReleases{
		Directory: dir,
		Store:     store,
		Keys:      []release.KeyPin{{PublicKey: pubB64, Components: []string{"proxy-agent"}}},
		Client:    server.Client(),
		API:       server.URL + "/repos/jingniao/qui",
	}

	entries1, err := svc.Import(context.Background(), "1.5.0", "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("First import failed: %v", err)
	}
	entries2, err := svc.Import(context.Background(), "1.5.0", "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("Second import failed: %v", err)
	}
	if len(entries1) != len(entries2) {
		t.Fatalf("expected idempotent entries, got %d and %d", len(entries1), len(entries2))
	}
}

func TestImportRejectsForeignSignature(t *testing.T) {
	pubTrusted, _ := generateSigningKey(t)
	_, privForeign := generateSigningKey(t)
	pubTrustedB64 := base64.StdEncoding.EncodeToString(pubTrusted)

	bin := []byte("binary")
	manifest, _ := createSignedAgentAsset(t, "1.5.0", "amd64", 1005000, privForeign, bin)

	fake := &fakeGitHub{
		lists: map[string]string{
			"jingniao/qui": `[
				{
					"tag_name": "v1.5.0",
					"published_at": "2026-11-09T00:00:00Z",
					"assets": [
						{"name": "proxy-agent-1.5.0-linux-amd64.bin", "browser_download_url": "/download/amd64.bin"},
						{"name": "proxy-agent-1.5.0-linux-amd64.manifest.json", "browser_download_url": "/download/amd64.json"}
					]
				}
			]`,
		},
		files: map[string]string{
			"/download/amd64.bin":  string(bin),
			"/download/amd64.json": manifest,
		},
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	svc := &AgentReleases{
		Directory: t.TempDir(),
		Store:     &fakeAgentReleaseStore{},
		Keys:      []release.KeyPin{{PublicKey: pubTrustedB64, Components: []string{"proxy-agent"}}},
		Client:    server.Client(),
		API:       server.URL + "/repos/jingniao/qui",
	}

	_, err := svc.Import(context.Background(), "1.5.0", "admin", "127.0.0.1")
	if err == nil {
		t.Fatal("expected error importing release with foreign signature")
	}
}

func TestImportRejectsDigestMismatch(t *testing.T) {
	pub, priv := generateSigningKey(t)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	bin := []byte("original binary")
	manifest, _ := createSignedAgentAsset(t, "1.5.0", "amd64", 1005000, priv, bin)
	corruptBin := []byte("corrupted binary")

	fake := &fakeGitHub{
		lists: map[string]string{
			"jingniao/qui": `[
				{
					"tag_name": "v1.5.0",
					"published_at": "2026-11-09T00:00:00Z",
					"assets": [
						{"name": "proxy-agent-1.5.0-linux-amd64.bin", "browser_download_url": "/download/amd64.bin"},
						{"name": "proxy-agent-1.5.0-linux-amd64.manifest.json", "browser_download_url": "/download/amd64.json"}
					]
				}
			]`,
		},
		files: map[string]string{
			"/download/amd64.bin":  string(corruptBin),
			"/download/amd64.json": manifest,
		},
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	svc := &AgentReleases{
		Directory: t.TempDir(),
		Store:     &fakeAgentReleaseStore{},
		Keys:      []release.KeyPin{{PublicKey: pubB64, Components: []string{"proxy-agent"}}},
		Client:    server.Client(),
		API:       server.URL + "/repos/jingniao/qui",
	}

	_, err := svc.Import(context.Background(), "1.5.0", "admin", "127.0.0.1")
	if err == nil {
		t.Fatal("expected error importing release with mismatched digest")
	}
}

func TestImportFailureIsFixedCode(t *testing.T) {
	fake := &fakeGitHub{
		lists: map[string]string{}, // Will answer 500 with "upstream secret text"
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	svc := &AgentReleases{
		Directory: t.TempDir(),
		Store:     &fakeAgentReleaseStore{},
		Client:    server.Client(),
		API:       server.URL + "/repos/jingniao/qui",
	}

	handler := &AgentReleaseHandler{
		Releases: svc,
		Auth:     &auth.Handler{},
	}

	req := httptest.NewRequest("POST", "/api/v1/agent-releases/1.5.0/import", nil)
	req.SetPathValue("version", "1.5.0")
	rec := httptest.NewRecorder()
	handler.importRelease(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "agent_release_import_failed") {
		t.Fatalf("expected agent_release_import_failed code, got %s", body)
	}
	if strings.Contains(body, "upstream secret text") {
		t.Fatalf("response leaked upstream text: %s", body)
	}
}

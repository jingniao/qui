package policy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
)

type fakeFetcher struct {
	fetchFunc func(ctx context.Context, url string) ([]byte, error)
}

func (f *fakeFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	return f.fetchFunc(ctx, url)
}

func setupPolicyRepo(t *testing.T) (*policy.Repository, *vault.Vault) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	v, err := vault.Open(filepath.Join(dir, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	return &policy.Repository{Database: database, Vault: v}, v
}

func TestSaveSourcesResetsCacheForChangedURL(t *testing.T) {
	ctx := context.Background()
	repo, v := setupPolicyRepo(t)

	// Create a service set
	svc, err := repo.SaveService(ctx, "", policy.ServiceInput{
		Name:    "Test Service",
		Enabled: true,
		Domains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("SaveService failed: %v", err)
	}

	// Insert a source directly with sealed cache and ok status
	cacheRaw, _ := json.Marshal(policy.ParsedEntries{Domains: []string{"cached.example.com"}})
	sealedCache, err := v.Seal(cacheRaw)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := repo.Database.ExecContext(ctx,
		`INSERT INTO service_set_sources(id, service_id, url, format, behavior, interval_seconds, position, last_status, last_error_code, last_fetch_at, last_success_at, entry_count, skipped_count, encrypted_cache, created_at, updated_at)
		 VALUES('src-1', ?, 'https://example.com/rules.txt', 'text', 'domain', 86400, 0, 'ok', 'previous_error', ?, ?, 1, 2, ?, ?, ?)`,
		svc.ID, now, now, sealedCache, now, now); err != nil {
		t.Fatal(err)
	}

	// Update sources with the same ID but updated URL
	inputs := []policy.SourceInput{
		{
			ID:              "src-1",
			URL:             "https://example.com/rules-v2.txt",
			Format:          "text",
			Behavior:        "domain",
			IntervalSeconds: 86400,
		},
	}
	if err := repo.SaveSources(ctx, nil, svc.ID, inputs); err != nil {
		t.Fatalf("SaveSources failed: %v", err)
	}

	// A changed identity must not reuse the previous source's cache.
	updatedSvc, err := repo.Service(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updatedSvc.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(updatedSvc.Sources))
	}
	src := updatedSvc.Sources[0]
	if src.ID != "src-1" || src.URL != "https://example.com/rules-v2.txt" {
		t.Errorf("unexpected source url or id: %+v", src)
	}
	if src.LastStatus != "pending" || src.EntryCount != 0 || src.SkippedCount != 0 || src.LastErrorCode != "" || src.LastFetchAt != nil || src.LastSuccessAt != nil {
		t.Errorf("expected pending source with cleared metadata, got %+v", src)
	}

	// Decrypt entries via SourceEntries
	entries, err := repo.SourceEntries(ctx, svc.ID)
	if err != nil {
		t.Fatalf("SourceEntries failed: %v", err)
	}
	if len(entries.Domains) != 0 {
		t.Errorf("unexpected cached entries: %+v", entries)
	}
	var created string
	if err := repo.Database.QueryRowContext(ctx, "SELECT created_at FROM service_set_sources WHERE id='src-1'").Scan(&created); err != nil || created != now {
		t.Fatalf("created_at changed: %q %v", created, err)
	}
}

func TestRefreshStoresSealedCache(t *testing.T) {
	ctx := context.Background()
	repo, v := setupPolicyRepo(t)

	svc, err := repo.SaveService(ctx, "", policy.ServiceInput{
		Name:    "Sealed Cache Svc",
		Enabled: true,
		Domains: []string{"base.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	sourceInputs := []policy.SourceInput{
		{
			URL:             "https://example.com/rules.txt",
			Format:          "text",
			Behavior:        "classical",
			IntervalSeconds: 86400,
		},
	}
	if err := repo.SaveSources(ctx, nil, svc.ID, sourceInputs); err != nil {
		t.Fatal(err)
	}

	updatedSvc, err := repo.Service(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	sourceID := updatedSvc.Sources[0].ID

	fetcher := &fakeFetcher{
		fetchFunc: func(ctx context.Context, url string) ([]byte, error) {
			return []byte("DOMAIN,secret.example.com\nDOMAIN-KEYWORD,openai\n"), nil
		},
	}

	refresher := &policy.SourceRefresher{
		Repository: repo,
		Fetcher:    fetcher,
		Vault:      v,
		Now:        time.Now,
	}

	res, err := refresher.Refresh(ctx, sourceID)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if res.LastStatus != "ok" {
		t.Errorf("expected status 'ok', got %s", res.LastStatus)
	}

	// Verify database row contains encrypted bytes without plaintext
	var encryptedBlob []byte
	if err := repo.Database.QueryRowContext(ctx, "SELECT encrypted_cache FROM service_set_sources WHERE id=?", sourceID).Scan(&encryptedBlob); err != nil {
		t.Fatal(err)
	}
	if len(encryptedBlob) == 0 {
		t.Fatal("expected non-empty encrypted_cache")
	}
	if bytes.Contains(encryptedBlob, []byte("secret.example.com")) {
		t.Error("database encrypted_cache contains plaintext domain!")
	}

	// Verify SourceEntries can decrypt it
	entries, err := repo.SourceEntries(ctx, svc.ID)
	if err != nil {
		t.Fatalf("SourceEntries failed: %v", err)
	}
	if len(entries.Domains) != 1 || entries.Domains[0] != "secret.example.com" {
		t.Errorf("unexpected decrypted domains: %v", entries.Domains)
	}
	if len(entries.Keywords) != 1 || entries.Keywords[0] != "openai" {
		t.Errorf("unexpected decrypted keywords: %v", entries.Keywords)
	}
}

func TestSaveSourcesIdentityAndIntervalChanges(t *testing.T) {
	for _, change := range []string{"interval", "format", "behavior"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			repo, v := setupPolicyRepo(t)
			sources := []policy.SourceInput{{URL: "https://example.com/rules", Format: "text", Behavior: "domain", IntervalSeconds: 3600}}
			set, err := repo.SaveService(ctx, "", policy.ServiceInput{Name: "identity", Enabled: true, Sources: &sources})
			if err != nil {
				t.Fatal(err)
			}
			refresher := &policy.SourceRefresher{Repository: repo, Vault: v, Fetcher: &fakeFetcher{fetchFunc: func(context.Context, string) ([]byte, error) { return []byte("example.com\n"), nil }}}
			if _, err := refresher.Refresh(ctx, set.Sources[0].ID); err != nil {
				t.Fatal(err)
			}
			sources[0].ID = set.Sources[0].ID
			switch change {
			case "interval":
				sources[0].IntervalSeconds = 7200
			case "format":
				sources[0].Format = "yaml"
			case "behavior":
				sources[0].Behavior = "classical"
			}
			if err := repo.SaveSources(ctx, nil, set.ID, sources); err != nil {
				t.Fatal(err)
			}
			updated, err := repo.Service(ctx, set.ID)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := repo.SourceEntries(ctx, set.ID)
			if err != nil {
				t.Fatal(err)
			}
			if change == "interval" {
				if updated.Sources[0].LastStatus != "ok" || len(entries.Domains) != 1 {
					t.Fatalf("unchanged identity lost cache: %+v %+v", updated.Sources, entries)
				}
			} else if updated.Sources[0].LastStatus != "pending" || updated.Sources[0].LastFetchAt != nil || len(entries.Domains) != 0 {
				t.Fatalf("changed identity kept cache: %+v %+v", updated.Sources, entries)
			}
		})
	}
}

func TestRefreshKeepsCacheOnEmptyParse(t *testing.T) {
	ctx := context.Background()
	repo, v := setupPolicyRepo(t)

	svc, err := repo.SaveService(ctx, "", policy.ServiceInput{
		Name:    "Empty Parse Svc",
		Enabled: true,
		Domains: []string{"base.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// First refresh with valid data
	if err := repo.SaveSources(ctx, nil, svc.ID, []policy.SourceInput{
		{
			URL:             "https://example.com/rules.txt",
			Format:          "text",
			Behavior:        "domain",
			IntervalSeconds: 86400,
		},
	}); err != nil {
		t.Fatal(err)
	}
	updatedSvc, err := repo.Service(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	sourceID := updatedSvc.Sources[0].ID

	body := []byte("old.example.com\n")
	fetcher := &fakeFetcher{
		fetchFunc: func(ctx context.Context, url string) ([]byte, error) {
			return body, nil
		},
	}
	refresher := &policy.SourceRefresher{
		Repository: repo,
		Fetcher:    fetcher,
		Vault:      v,
		Now:        time.Now,
	}

	if _, err := refresher.Refresh(ctx, sourceID); err != nil {
		t.Fatal(err)
	}

	// Now fetcher returns HTML error page (200 OK but 0 valid entries)
	body = []byte("<!DOCTYPE html><html><body>Error 404 Not Found</body></html>")
	res, err := refresher.Refresh(ctx, sourceID)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if res.LastStatus != "failed" || res.LastErrorCode != "parse_empty" {
		t.Errorf("expected status 'failed' with code 'parse_empty', got status=%s code=%s", res.LastStatus, res.LastErrorCode)
	}

	// SourceEntries should still return old cache
	entries, err := repo.SourceEntries(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries.Domains) != 1 || entries.Domains[0] != "old.example.com" {
		t.Errorf("expected old cache preserved, got %v", entries.Domains)
	}
}

func TestRefreshRecordsFetchCode(t *testing.T) {
	ctx := context.Background()
	repo, v := setupPolicyRepo(t)

	svc, err := repo.SaveService(ctx, "", policy.ServiceInput{
		Name:    "Fetch Code Svc",
		Enabled: true,
		Domains: []string{"base.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.SaveSources(ctx, nil, svc.ID, []policy.SourceInput{
		{
			URL:             "https://example.com/rules.txt",
			Format:          "text",
			Behavior:        "domain",
			IntervalSeconds: 86400,
		},
	}); err != nil {
		t.Fatal(err)
	}
	updatedSvc, err := repo.Service(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	sourceID := updatedSvc.Sources[0].ID

	fetcher := &fakeFetcher{
		fetchFunc: func(ctx context.Context, url string) ([]byte, error) {
			return nil, &importer.FetchError{Code: "source_dns_failed", Stage: "dns"}
		},
	}
	refresher := &policy.SourceRefresher{
		Repository: repo,
		Fetcher:    fetcher,
		Vault:      v,
		Now:        time.Now,
	}

	res, err := refresher.Refresh(ctx, sourceID)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if res.LastStatus != "failed" || res.LastErrorCode != "source_dns_failed" {
		t.Errorf("expected failed with source_dns_failed, got status=%s code=%s", res.LastStatus, res.LastErrorCode)
	}
}

func TestMRSSourceIsNotFetchedForNodes(t *testing.T) {
	ctx := context.Background()
	repo, v := setupPolicyRepo(t)

	svc, err := repo.SaveService(ctx, "", policy.ServiceInput{
		Name:    "MRS Svc",
		Enabled: true,
		Domains: []string{"base.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.SaveSources(ctx, nil, svc.ID, []policy.SourceInput{
		{
			URL:             "https://example.com/rules.mrs",
			Format:          "mrs",
			Behavior:        "domain",
			IntervalSeconds: 86400,
		},
	}); err != nil {
		t.Fatal(err)
	}
	updatedSvc, err := repo.Service(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	source := updatedSvc.Sources[0]
	if source.NodeUsable {
		t.Error("expected NodeUsable=false for mrs source")
	}
	if !source.ClientUsable {
		t.Error("expected ClientUsable=true for mrs source")
	}

	fetched := false
	fetcher := &fakeFetcher{
		fetchFunc: func(ctx context.Context, url string) ([]byte, error) {
			fetched = true
			return []byte("data"), nil
		},
	}
	refresher := &policy.SourceRefresher{
		Repository: repo,
		Fetcher:    fetcher,
		Vault:      v,
		Now:        time.Now,
	}

	res, err := refresher.Refresh(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched {
		t.Error("expected mrs source NOT to be fetched")
	}
	if res.LastStatus != "pending" {
		t.Errorf("expected status to stay 'pending', got %s", res.LastStatus)
	}
}

func TestServiceKeywordsRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPolicyRepo(t)

	svc, err := repo.SaveService(ctx, "", policy.ServiceInput{
		Name:        "Keyword Svc",
		Enabled:     true,
		Keywords:    []string{"openai", "chatgpt"},
		Description: "keyword test",
	})
	if err != nil {
		t.Fatalf("SaveService failed: %v", err)
	}

	readSvc, err := repo.Service(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(readSvc.Keywords) != 2 {
		t.Fatalf("expected 2 keywords, got %d", len(readSvc.Keywords))
	}
	if readSvc.Keywords[0] != "chatgpt" || readSvc.Keywords[1] != "openai" {
		t.Errorf("unexpected keywords: %v", readSvc.Keywords)
	}
}

func TestSaveServiceAcceptsSourcesOnly(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPolicyRepo(t)

	sources := []policy.SourceInput{
		{
			URL:             "https://example.com/rules.txt",
			Format:          "text",
			Behavior:        "domain",
			IntervalSeconds: 86400,
		},
	}

	svc, err := repo.SaveService(ctx, "", policy.ServiceInput{
		Name:    "Sources Only Svc",
		Enabled: true,
		Sources: &sources,
	})
	if err != nil {
		t.Fatalf("expected SaveService to accept sources only, got: %v", err)
	}
	if len(svc.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(svc.Sources))
	}
	if svc.Sources[0].URL != "https://example.com/rules.txt" {
		t.Errorf("unexpected source url: %s", svc.Sources[0].URL)
	}
}

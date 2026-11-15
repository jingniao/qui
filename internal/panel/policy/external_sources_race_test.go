package policy_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"proxycontrol/internal/panel/policy"
)

func TestRefreshDiscardsResultsAfterSourceIdentityChanges(t *testing.T) {
	for _, outcome := range []string{"success", "fetch_failed", "parse_failed"} {
		for _, field := range []string{"url", "format", "behavior"} {
			t.Run(outcome+"/"+field, func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				repo, v := setupPolicyRepo(t)
				service, err := repo.SaveService(ctx, "", policy.ServiceInput{Name: "Concurrent source", Enabled: true, Domains: []string{"example.com"}})
				if err != nil {
					t.Fatal(err)
				}
				if err := repo.SaveSources(ctx, nil, service.ID, []policy.SourceInput{{URL: "https://example.com/old.txt", Format: "text", Behavior: "domain", IntervalSeconds: 86400}}); err != nil {
					t.Fatal(err)
				}
				service, err = repo.Service(ctx, service.ID)
				if err != nil {
					t.Fatal(err)
				}
				sourceID := service.Sources[0].ID
				readState := func() []any {
					t.Helper()
					values := make([]any, 11)
					pointers := make([]any, len(values))
					for i := range values {
						pointers[i] = &values[i]
					}
					if err := repo.Database.QueryRowContext(ctx, `SELECT url,format,behavior,last_status,last_error_code,entry_count,skipped_count,encrypted_cache,last_fetch_at,last_success_at,updated_at FROM service_set_sources WHERE id=?`, sourceID).Scan(pointers...); err != nil {
						t.Fatal(err)
					}
					return values
				}
				var expected []any
				changes := 0
				refresher := &policy.SourceRefresher{Repository: repo, Vault: v, OnChange: func(string) { changes++ }}
				refresher.Fetcher = &fakeFetcher{fetchFunc: func(context.Context, string) ([]byte, error) {
					value := map[string]string{"url": "https://example.com/new.txt", "format": "yaml", "behavior": "classical"}[field]
					if _, err := repo.Database.ExecContext(ctx, "UPDATE service_set_sources SET "+field+"=?,last_status='pending',last_error_code='new_source',entry_count=7,skipped_count=3,updated_at='changed' WHERE id=?", value, sourceID); err != nil {
						t.Fatal(err)
					}
					expected = readState()
					switch outcome {
					case "fetch_failed":
						return nil, errors.New("old source unavailable")
					case "parse_failed":
						return []byte("# no entries\n"), nil
					default:
						return []byte("old.example.com\n"), nil
					}
				}}
				if _, err := refresher.Refresh(ctx, sourceID); err != nil {
					t.Fatal(err)
				}
				if actual := readState(); !reflect.DeepEqual(actual, expected) {
					t.Fatalf("stale fetch changed source: got %#v, want %#v", actual, expected)
				}
				if changes != 0 {
					t.Fatalf("stale fetch emitted %d changes", changes)
				}
			})
		}
	}
}

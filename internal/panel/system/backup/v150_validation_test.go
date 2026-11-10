package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/policy"
)

func TestInspectRejectsInvalidV150Tables(t *testing.T) {
	for _, scenario := range []string{"undecryptable cache", "invalid cache JSON", "unknown dismissal", "empty dismissal", "oversized dismissal"} {
		t.Run(scenario, func(t *testing.T) {
			source, directory, _ := sourceFixture(t)
			ctx := context.Background()
			if strings.Contains(scenario, "cache") {
				sources := []policy.SourceInput{{URL: "https://example.com/rules", Format: "text", Behavior: "domain"}}
				repo := &policy.Repository{Database: source.Database, Vault: source.ConfigurationVault}
				set, err := repo.SaveService(ctx, "", policy.ServiceInput{Name: "archive source", Enabled: true, Sources: &sources})
				if err != nil {
					t.Fatal(err)
				}
				cache := []byte("unreadable cache")
				if scenario == "invalid cache JSON" {
					cache, err = source.ConfigurationVault.Seal([]byte(`{"domains":123}`))
					if err != nil {
						t.Fatal(err)
					}
				}
				if _, err := source.Database.ExecContext(ctx, "UPDATE service_set_sources SET encrypted_cache=? WHERE service_id=?", cache, set.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				key, value := "net.ipv4.tcp_ecn", "2"
				switch scenario {
				case "unknown dismissal":
					key = "kernel.arbitrary_unknown"
				case "empty dismissal":
					value = ""
				case "oversized dismissal":
					value = strings.Repeat("x", 257)
				}
				if _, err := source.Database.ExecContext(ctx, "INSERT INTO node_tuning_dismissals(node_id,key,value,rules_version,dismissed_at) SELECT id,?,?, '1', '2026-11-10T00:00:00Z' FROM nodes LIMIT 1", key, value); err != nil {
					t.Fatal(err)
				}
			}
			material, err := source.snapshot(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{}
			for _, file := range material {
				data, err := os.ReadFile(file.source)
				if err != nil {
					t.Fatal(err)
				}
				files[file.Name] = data
			}
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "invalid_v150_archive.pcb")
			repackFixture(t, path, Manifest{Version: 1, ID: "invalid_v150_archive", CreatedAt: time.Now().UTC(), CreatedBy: "owner"}, files, "")
			if _, err := Inspect(ctx, directory, path, testPassword); err == nil {
				t.Fatal("invalid table accepted by full archive inspection")
			}
		})
	}
}

func TestInspectAcceptsPre0140Archive(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	rewindServiceSetSources(t, source.Database)
	created, err := source.Create(context.Background(), directory, "pre0140_archive_01", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(context.Background(), directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword); err != nil {
		t.Fatal(err)
	}
}

func TestInspectAcceptsValidV150Tables(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	sources := []policy.SourceInput{{URL: "https://example.com/rules", Format: "yaml", Behavior: "classical"}}
	repo := &policy.Repository{Database: source.Database, Vault: source.ConfigurationVault}
	set, err := repo.SaveService(ctx, "", policy.ServiceInput{Name: "valid source", Enabled: true, Sources: &sources})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(policy.ParsedEntries{Domains: []string{"example.com"}})
	cache, err := source.ConfigurationVault.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Database.ExecContext(ctx, "UPDATE service_set_sources SET encrypted_cache=? WHERE service_id=?", cache, set.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Database.ExecContext(ctx, "INSERT INTO node_tuning_dismissals(node_id,key,value,rules_version,dismissed_at) SELECT id,'net.ipv4.tcp_ecn','2','1','2026-11-10T00:00:00Z' FROM nodes LIMIT 1"); err != nil {
		t.Fatal(err)
	}
	created, err := source.Create(ctx, directory, "valid_v150_archive", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword); err != nil {
		t.Fatal(err)
	}
}

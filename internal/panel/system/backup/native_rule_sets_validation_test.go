package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/policy"
)

func nativeSource() policy.SourceInput {
	return policy.SourceInput{
		URL:      "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs",
		Format:   "native",
		Behavior: "ipcidr",
		XrayURL:  "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat",
		XrayTag:  "cn",
	}
}

func TestInspectAcceptsNativeRuleSetSources(t *testing.T) {
	t.Parallel()
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	sources := []policy.SourceInput{nativeSource()}
	repo := &policy.Repository{Database: source.Database, Vault: source.ConfigurationVault}
	if _, err := repo.SaveService(ctx, "", policy.ServiceInput{Name: "native archive", Enabled: true, Sources: &sources}); err != nil {
		t.Fatal(err)
	}
	created, err := source.Create(ctx, directory, "native_archive_ok", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword); err != nil {
		t.Fatal(err)
	}
}

func TestInspectAcceptsPre0143Archive(t *testing.T) {
	t.Parallel()
	source, directory, _ := sourceFixture(t)
	rewindNativeRuleSets(t, source.Database)
	created, err := source.Create(context.Background(), directory, "pre0143_archive_01", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(context.Background(), directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword); err != nil {
		t.Fatal(err)
	}
}

func TestInspectRejectsInvalidNativeRuleSetSources(t *testing.T) {
	t.Parallel()
	// The table's CHECKs already cover the shape; these pass them and fail the
	// shared address and behavior rules.
	for name, update := range map[string]string{
		"foreign host":     "UPDATE service_set_sources SET url='https://example.com/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs' WHERE id='builtin-service-cn-native-1'",
		"foreign xray":     "UPDATE service_set_sources SET xray_url='https://example.com/geosite.dat' WHERE id='builtin-service-cn-native-1'",
		"behavior vs file": "UPDATE service_set_sources SET behavior='ipcidr' WHERE id='builtin-service-cn-native-1'",
		"bad tag":          "UPDATE service_set_sources SET xray_tag='CN CN' WHERE id='builtin-service-cn-native-1'",
	} {
		t.Run(name, func(t *testing.T) {
			source, directory, _ := sourceFixture(t)
			ctx := context.Background()
			if _, err := source.Database.ExecContext(ctx, update); err != nil {
				t.Fatal(err)
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
			path := filepath.Join(directory, "invalid_native_archive.pcb")
			repackFixture(t, path, Manifest{Version: 1, ID: "invalid_native_archive", CreatedAt: time.Now().UTC(), CreatedBy: "owner"}, files, "")
			if _, err := Inspect(ctx, directory, path, testPassword); err == nil {
				t.Fatal("invalid native source accepted by full archive inspection")
			}
		})
	}
}

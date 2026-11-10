package agenthost

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/internal/platform/config"
	"proxycontrol/pkg/contracts/release"
)

func TestEffectiveUpdatesKeepsExplicitConfig(t *testing.T) {
	c := config.Agent{
		DataDirectory: t.TempDir(),
		Updates: config.Updates{
			Catalogue: "https://custom.example.com/cat/",
			CA:        "custom-ca",
			Keys: []release.KeyPin{
				{PublicKey: "custom-key", Components: []string{"proxy-agent"}},
			},
		},
	}
	updates, ok, err := EffectiveUpdates(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok to be true")
	}
	if !reflect.DeepEqual(updates, c.Updates) {
		t.Fatalf("expected %+v, got %+v", c.Updates, updates)
	}
}

func TestEffectiveUpdatesBuildsPanelCatalogue(t *testing.T) {
	dir := t.TempDir()
	if err := SaveCataloguePort(dir, 443); err != nil {
		t.Fatalf("SaveCataloguePort failed: %v", err)
	}

	c := config.Agent{
		DataDirectory:  dir,
		ControlAddress: "panel.example.com:9443",
		TLS: config.TLS{
			ClientCA: "test-client-ca",
		},
	}
	updates, ok, err := EffectiveUpdates(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok to be true")
	}
	expectedCatalogue := "https://panel.example.com:443/agent-catalogue/"
	if updates.Catalogue != expectedCatalogue {
		t.Fatalf("expected catalogue %s, got %s", expectedCatalogue, updates.Catalogue)
	}
	if updates.CA != c.TLS.ClientCA {
		t.Fatalf("expected CA %s, got %s", c.TLS.ClientCA, updates.CA)
	}
	if !reflect.DeepEqual(updates.Keys, release.OfficialKeyPins) {
		t.Fatalf("expected keys %+v, got %+v", release.OfficialKeyPins, updates.Keys)
	}
}

func TestEffectiveUpdatesIPv6(t *testing.T) {
	dir := t.TempDir()
	if err := SaveCataloguePort(dir, 8443); err != nil {
		t.Fatalf("SaveCataloguePort failed: %v", err)
	}

	c := config.Agent{
		DataDirectory:  dir,
		ControlAddress: "[2001:db8::1]:9443",
		TLS: config.TLS{
			ClientCA: "test-client-ca",
		},
	}
	updates, ok, err := EffectiveUpdates(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok to be true")
	}
	expectedCatalogue := "https://[2001:db8::1]:8443/agent-catalogue/"
	if updates.Catalogue != expectedCatalogue {
		t.Fatalf("expected catalogue %s, got %s", expectedCatalogue, updates.Catalogue)
	}
}

func TestEffectiveUpdatesWithoutPort(t *testing.T) {
	c := config.Agent{
		DataDirectory:  t.TempDir(),
		ControlAddress: "panel.example.com:9443",
	}
	_, ok, err := EffectiveUpdates(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok to be false when no port saved")
	}
}

func TestSaveCataloguePortRejectsZeroAndRemoves(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, CataloguePortFile)

	if err := SaveCataloguePort(dir, 1234); err != nil {
		t.Fatalf("SaveCataloguePort failed: %v", err)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected %s to exist: %v", filePath, err)
	}

	// Saving port 0 should remove the file
	if err := SaveCataloguePort(dir, 0); err != nil {
		t.Fatalf("SaveCataloguePort(0) failed: %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, stat err: %v", err)
	}

	// Saving port 0 again on nonexistent file should not error
	if err := SaveCataloguePort(dir, 0); err != nil {
		t.Fatalf("SaveCataloguePort(0) on nonexistent file failed: %v", err)
	}
}

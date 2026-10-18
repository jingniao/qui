package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/config"
)

// Startup with a switching installation that was never activated rolls back to
// the retained image, records the rollback and releases the node.
func TestStartupRollsBackPendingCoreInstallation(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	_, cfg, manager, original := updateHarness(t, adapter)
	ctx := context.Background()
	prepareSwitchingInstallation(t, ctx, cfg, original)
	installation, err := openCoreInstallation(ctx, cfg, "node-update")
	if err != nil {
		t.Fatal(err)
	}
	if err := repairCoreInstallation(ctx, cfg, installation, manager); err != nil {
		t.Fatalf("startup rollback failed: %v", err)
	}
	installation.Close()
	history, err := openCoreHistory(cfg, "node-update")
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	repaired, ok, err := history.Installation("xray")
	if err != nil || !ok || repaired.Phase != "rolled_back" || repaired.Current.Sequence != 0 {
		t.Fatalf("startup did not record the rollback: %+v %v", repaired, err)
	}
	if manager.CoreUpdateHeld() {
		t.Fatal("startup left the node held")
	}
	if adapter.selected == "" {
		t.Fatal("startup did not reselect the retained image")
	}
}

// prepareSwitchingInstallation records a baseline image and an interrupted
// switch, leaving no open history handle behind.
func prepareSwitchingInstallation(t *testing.T, ctx context.Context, cfg config.Agent, original string) {
	t.Helper()
	history, err := openCoreHistory(cfg, "node-update")
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	baseline, err := updater.InspectImage(ctx, "xray", original, "26.3.27")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updater.MaterializeImage(ctx, filepath.Join(cfg.DataDirectory, "releases", "installed"), original, "xray", baseline); err != nil {
		t.Fatal(err)
	}
	if err := history.BootstrapImage(ctx, "xray", baseline); err != nil {
		t.Fatal(err)
	}
	target := baseline
	target.SHA256 = strings.Repeat("ab", 32)
	target.Sequence = 1
	if _, err := history.BeginInstallationImage(ctx, "xray", "upd_interrupted", 1, target); err != nil {
		t.Fatal(err)
	}
}

// Startup with an unreadable hold marker preserves the damaged bytes, repairs
// the marker and continues.
func TestStartupRepairsUnreadableHoldMarker(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	_, cfg, manager, _ := updateHarness(t, adapter, func(dataDirectory string) {
		if err := os.WriteFile(filepath.Join(dataDirectory, "core-update-hold.enc"), []byte("damaged"), 0600); err != nil {
			t.Fatal(err)
		}
	})
	ctx := context.Background()
	if !manager.CoreUpdateHeld() {
		t.Fatal("damaged marker did not hold the node")
	}
	installation, err := openCoreInstallation(ctx, cfg, "node-update")
	if err != nil {
		t.Fatal(err)
	}
	defer installation.Close()
	if err := repairCoreInstallation(ctx, cfg, installation, manager); err != nil {
		t.Fatalf("hold repair failed: %v", err)
	}
	if manager.CoreUpdateHeld() {
		t.Fatal("hold not released after repair")
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDirectory, "core-update-hold.enc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("damaged marker not replaced: %v", err)
	}
	evidence, err := os.ReadFile(filepath.Join(cfg.DataDirectory, "release-hold-recovery", "core-update-hold.enc.damaged"))
	if err != nil || string(evidence) != "damaged" {
		t.Fatalf("damaged marker evidence missing: %q %v", evidence, err)
	}
}

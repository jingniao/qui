package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core/xray"
	"testing"
)

func TestRealManifestFailureRollsBackAndCachesResult(t *testing.T) {
	path := os.Getenv("XRAY_BINARY")
	if path == "" {
		t.Skip("set XRAY_BINARY for persistence fault integration")
	}
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &xray.Adapter{NodeID: "node-test", Path: path, Directory: filepath.Join(directory, "runtime")}
	defer adapter.Close()
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	first := state(freePort(t), 1)
	if result, err := manager.Apply(ctx, first); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	manifestPath := filepath.Join(directory, "current.json")
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifestPath, 0700); err != nil {
		t.Fatal(err)
	}
	next := state(freePort(t), 2)
	result, err := manager.Apply(ctx, next)
	if err == nil || result.Success || !result.RolledBack || result.Code != "persistence_failed" {
		t.Fatalf("persistence failure result wrong: %+v %v", result, err)
	}
	duplicate, err := manager.Apply(ctx, next)
	if err == nil || duplicate.Success || duplicate.Code != result.Code || !duplicate.RolledBack {
		t.Fatalf("duplicate result changed: %+v %v", duplicate, err)
	}
	if manager.Current() != 1 {
		t.Fatal("manifest failure changed active revision")
	}
	if health, err := adapter.Health(ctx); err != nil || !health.Healthy {
		t.Fatal("prior core not healthy after persistence failure", err)
	}
}

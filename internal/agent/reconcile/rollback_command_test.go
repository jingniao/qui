package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/xray"
	"testing"
)

type receiptFaultAdapter struct {
	*xray.Adapter
	onApply func()
}

func (a *receiptFaultAdapter) Apply(ctx context.Context, rendered core.RenderedConfig) (core.ApplyResult, error) {
	result, err := a.Adapter.Apply(ctx, rendered)
	if err == nil && a.onApply != nil {
		hook := a.onApply
		a.onApply = nil
		hook()
	}
	return result, err
}

func TestRealRollbackCommandReceiptFailureRestoresPriorRuntime(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for rollback persistence fault integration")
	}
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &receiptFaultAdapter{Adapter: &xray.Adapter{NodeID: "node-test", Path: binary, Directory: filepath.Join(directory, "runtime")}}
	defer adapter.Close()
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	for revision := int64(1); revision <= 2; revision++ {
		if result, err := manager.Apply(ctx, state(freePort(t), revision)); err != nil || !result.Success {
			t.Fatal(result, err)
		}
	}
	// Block the atomic receipt rename after the actual runtime switch. The
	// command must compensate instead of reporting failure with the new runtime.
	adapter.onApply = func() {
		if err := os.Mkdir(filepath.Join(directory, "commands", "receipt-fault.json"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if success, code, err := manager.RollbackCommand(ctx, "receipt-fault", 1); success || code != "persistence_failed" || err == nil {
		t.Fatal("receipt fault was not reported", success, code, err)
	}
	if manager.Current() != 2 {
		t.Fatal("receipt failure changed current successful revision")
	}
	if health, err := adapter.Health(ctx); err != nil || !health.Healthy {
		t.Fatal("receipt failure left prior runtime unhealthy", err)
	}
	if err := os.Remove(filepath.Join(directory, "commands", "receipt-fault.json")); err != nil {
		t.Fatal(err)
	}
	if success, _, err := manager.RollbackCommand(ctx, "valid-command", 1); !success || err != nil {
		t.Fatal("valid rollback failed", err)
	}
	if result, err := manager.Apply(ctx, state(freePort(t), 3)); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if success, _, err := manager.RollbackCommand(ctx, "valid-command", 1); !success || err != nil || manager.Current() != 3 {
		t.Fatal("duplicate old command overwrote newer runtime", err)
	}
	if _, _, err := manager.RollbackCommand(ctx, "valid-command", 2); err == nil {
		t.Fatal("same command ID changed revision")
	}
	if success, _, err := manager.RollbackCommand(ctx, "reset-command", 0); !success || err != nil || manager.Current() != 0 {
		t.Fatal("first-deployment reset failed", err)
	}
	restarted, err := New(directory, "node-test", adapter)
	if err != nil || restarted.Current() != 0 {
		t.Fatal("reset not persistent", err)
	}
	if err := restarted.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if health, _ := adapter.Health(ctx); health.Healthy {
		t.Fatal("reset startup resurrected a retained core")
	}
}

func TestRealRollbackManifestFailureRestoresPriorCore(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for rollback manifest fault integration")
	}
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &xray.Adapter{NodeID: "node-test", Path: binary, Directory: filepath.Join(directory, "runtime")}
	defer adapter.Close()
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	for revision := int64(1); revision <= 2; revision++ {
		if result, err := manager.Apply(ctx, state(freePort(t), revision)); err != nil || !result.Success {
			t.Fatal(result, err)
		}
	}
	manifestPath := filepath.Join(directory, "current.json")
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifestPath, 0700); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []int64{1, 0} {
		commandID := "restore-fault"
		if revision == 0 {
			commandID = "reset-fault"
		}
		if success, _, err := manager.RollbackCommand(ctx, commandID, revision); success || err == nil {
			t.Fatal("manifest failure was not reported")
		}
		if manager.Current() != 2 {
			t.Fatal("manifest failure changed successful revision")
		}
		if health, err := adapter.Health(ctx); err != nil || !health.Healthy {
			t.Fatal("manifest failure stopped previous core", err)
		}
	}
}

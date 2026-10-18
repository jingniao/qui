package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/xray"
	"testing"
)

func TestControlOnlyRejectsEveryExecutableEntryPoint(t *testing.T) {
	r, err := NewControlOnly(t.TempDir(), "node-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if !r.CoreUpdateHeld() {
		t.Fatal("control-only instance not held")
	}
	if _, err := r.Apply(ctx, state(1080, 1)); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("apply", err)
	}
	if err := r.Restore(ctx); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("restore", err)
	}
	if err := r.Rollback(ctx, 1); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("rollback", err)
	}
	if _, _, err := r.RollbackCommand(ctx, "rollback-test", 1); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("command", err)
	}
	if _, err := r.Refresh(ctx); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("schedule", err)
	}
	invoked := false
	if err := r.WithCoreUpdate(ctx, 1, func(*CoreUpdateLease) error { invoked = true; return nil }); !errors.Is(err, core.ErrReleaseRecovery) || invoked {
		t.Fatal("update lease", err)
	}
}

func TestRealControlOnlyKeepsRevisionAndRequiresNewWorkerForProxy(t *testing.T) {
	path := os.Getenv("XRAY_BINARY")
	if path == "" {
		t.Skip("set actual Xray")
	}
	directory := t.TempDir()
	ctx := context.Background()
	adapter := &xray.Adapter{NodeID: "node-test", Path: path, Directory: filepath.Join(directory, "runtime")}
	defer adapter.Close()
	r, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	first := state(freePort(t), 1)
	if result, err := r.Apply(ctx, first); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	held, err := NewControlOnly(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if held.Current() != 1 {
		t.Fatal("control-only lost retained revision")
	}
	if err := held.Restore(ctx); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal(err)
	}
	if health, err := adapter.Health(ctx); err == nil && health.Healthy {
		t.Fatal("control-only started core")
	}
	fresh, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Restore(ctx); err != nil {
		t.Fatal("new verified worker cannot resume", err)
	}
	if health, err := adapter.Health(ctx); err != nil || !health.Healthy {
		t.Fatal(health, err)
	}
}

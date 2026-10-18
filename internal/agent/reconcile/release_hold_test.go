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

func TestRealReleaseExecutionHoldSurvivesRestartAndRejectsDamagedMarker(t *testing.T) {
	path := os.Getenv("XRAY_BINARY")
	if path == "" {
		t.Skip("set Xray for durable release hold")
	}
	for _, damage := range []bool{false, true} {
		t.Run(map[bool]string{false: "original", true: "damaged"}[damage], func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			adapter := &xray.Adapter{NodeID: "node-test", Path: path, Directory: filepath.Join(root, "runtime")}
			defer adapter.Close()
			manager, err := New(root, "node-test", adapter)
			if err != nil {
				t.Fatal(err)
			}
			desired := state(freePort(t), 1)
			if result, err := manager.Apply(ctx, desired); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			if err := manager.WithCoreUpdate(ctx, 1, func(lease *CoreUpdateLease) error { return errors.Join(lease.Hold(), lease.Stop(ctx)) }); err != nil {
				t.Fatal(err)
			}
			if damage {
				if err := os.WriteFile(filepath.Join(root, releaseHoldName), []byte("damaged"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			restarted, err := New(root, "node-test", adapter)
			if err != nil {
				t.Fatal(err)
			}
			if !restarted.CoreUpdateHeld() {
				t.Fatal("restart forgot execution hold")
			}
			if err := restarted.Restore(ctx); !errors.Is(err, core.ErrReleaseRecovery) {
				t.Fatal("normal startup reopened held core", err)
			}
			if _, err := restarted.Refresh(ctx); !errors.Is(err, core.ErrReleaseRecovery) {
				t.Fatal("time reconciliation bypassed durable hold", err)
			}
			if _, err := restarted.Apply(ctx, desired); !errors.Is(err, core.ErrReleaseRecovery) {
				t.Fatal("publication bypassed durable hold", err)
			}
			err = restarted.WithCoreUpdate(ctx, 1, func(lease *CoreUpdateLease) error {
				if err := lease.Activate(ctx, path); err != nil {
					return err
				}
				return lease.Release()
			})
			if damage {
				if !errors.Is(err, core.ErrReleaseRecovery) {
					t.Fatal("damaged authenticated marker reset", err)
				}
				return
			}
			if err != nil || restarted.CoreUpdateHeld() {
				t.Fatal("confirmed fixed recovery did not release hold", err)
			}
			last, err := New(root, "node-test", adapter)
			if err != nil || last.CoreUpdateHeld() {
				t.Fatal("released hold persisted incorrectly", err)
			}
		})
	}
}

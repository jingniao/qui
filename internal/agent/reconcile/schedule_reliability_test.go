package reconcile

import (
	"context"
	"testing"
)

// TestFailedScheduledRefreshKeepsRunningRevision proves a failed time-window
// re-render leaves the running revision and core untouched and retries on the
// next tick. The pre-fix behaviour switched to block-all and stopped the core.
func TestFailedScheduledRefreshKeepsRunningRevision(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(1080, 1)
	if result, err := manager.Apply(ctx, configuration); err != nil || !result.Success {
		t.Fatalf("initial apply failed: %+v %v", result, err)
	}
	adapter.failRender = true
	changed, err := manager.Refresh(ctx)
	if err == nil || changed {
		t.Fatalf("failed refresh not reported: changed=%v err=%v", changed, err)
	}
	if manager.Current() != 1 {
		t.Fatal("failed refresh changed the running revision")
	}
	if adapter.closed {
		t.Fatal("failed refresh stopped the core")
	}
	applied := adapter.applyCalls
	adapter.failRender = false
	adapter.renderVariant = "v2"
	changed, err = manager.Refresh(ctx)
	if err != nil || !changed {
		t.Fatalf("next tick did not retry and apply: changed=%v err=%v", changed, err)
	}
	if adapter.applyCalls != applied+1 {
		t.Fatalf("retry applied %d times, want %d", adapter.applyCalls, applied+1)
	}
}

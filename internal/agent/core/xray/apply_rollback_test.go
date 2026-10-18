package xray

import (
	"context"
	"errors"
	"proxycontrol/internal/agent/core"
	"testing"
)

// TestApplySwitchRestartsPreviousAfterStartFailure proves every post-stop error
// path restarts the previous revision.
func TestApplySwitchRestartsPreviousAfterStartFailure(t *testing.T) {
	previous := core.RenderedConfig{Revision: 5}
	next := core.RenderedConfig{Revision: 6}
	var started []int64
	stopCalls := 0
	stop := func() error { stopCalls++; return nil }
	start := func(_ context.Context, configuration core.RenderedConfig) error {
		started = append(started, configuration.Revision)
		if configuration.Revision == next.Revision {
			return errors.New("start failed")
		}
		return nil
	}
	result := core.ApplyResult{Revision: next.Revision}
	if err := applySwitch(context.Background(), previous, next, &result, stop, start); err == nil {
		t.Fatal("start failure was not reported")
	}
	if stopCalls != 1 {
		t.Fatalf("stop called %d times", stopCalls)
	}
	if !result.RolledBack || result.Code != "start_failed" {
		t.Fatalf("previous was not restarted: %+v", result)
	}
	if len(started) != 2 || started[0] != next.Revision || started[1] != previous.Revision {
		t.Fatalf("start sequence = %v", started)
	}
}

// TestApplySwitchReportsRollbackFailure proves a failed restart of previous is
// reported as rollback_failed.
func TestApplySwitchReportsRollbackFailure(t *testing.T) {
	start := func(context.Context, core.RenderedConfig) error { return errors.New("cannot start") }
	result := core.ApplyResult{Revision: 6}
	if err := applySwitch(context.Background(), core.RenderedConfig{Revision: 5}, core.RenderedConfig{Revision: 6}, &result, func() error { return nil }, start); err == nil {
		t.Fatal("rollback failure was not reported")
	}
	if result.Code != "rollback_failed" || result.RolledBack {
		t.Fatalf("rollback failure result wrong: %+v", result)
	}
}

// TestApplySwitchStopFailureDoesNotStart proves a failed stop never attempts to
// start the candidate.
func TestApplySwitchStopFailureDoesNotStart(t *testing.T) {
	started := false
	start := func(context.Context, core.RenderedConfig) error { started = true; return nil }
	result := core.ApplyResult{Revision: 2}
	if err := applySwitch(context.Background(), core.RenderedConfig{Revision: 1}, core.RenderedConfig{Revision: 2}, &result, func() error { return errors.New("stop failed") }, start); err == nil {
		t.Fatal("stop failure was not reported")
	}
	if result.Code != "stop_failed" || started {
		t.Fatalf("stop failure handling wrong: %+v started=%v", result, started)
	}
}

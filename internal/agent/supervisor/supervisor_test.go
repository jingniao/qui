package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
	"time"
)

// fakeAdapter runs no process: the retained revision renders, but every apply
// after the first one fails, the way a core that cannot start the revision does.
type fakeAdapter struct {
	healthy     bool
	failApply   bool
	applyCalls  int
	renderCalls int
	// failRender fails every render, the way an installed core cannot render a
	// retained revision that was built for another one.
	failRender bool
	// failRefreshes fails that many renders and then renders again, so a refresh
	// that failed once recovers. It is set before Run starts, never while it runs.
	failRefreshes int
	// renderVariant changes the rendered bytes, which is what a refresh that has
	// to apply looks like.
	renderVariant string
}

func (a *fakeAdapter) Name() string { return "fake" }
func (a *fakeAdapter) DetectCapabilities(context.Context) (*pb.Capabilities, error) {
	return &pb.Capabilities{}, nil
}
func (a *fakeAdapter) Render(_ context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	a.renderCalls++
	if a.failRender {
		return core.RenderedConfig{}, errors.New("render failed")
	}
	if a.failRefreshes > 0 {
		a.failRefreshes--
		return core.RenderedConfig{}, errors.New("render failed")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	return core.RenderedConfig{Revision: state.Revision, Data: append(data, a.renderVariant...)}, nil
}
func (a *fakeAdapter) Validate(context.Context, core.RenderedConfig) error { return nil }
func (a *fakeAdapter) Apply(_ context.Context, configuration core.RenderedConfig) (core.ApplyResult, error) {
	a.applyCalls++
	if a.failApply {
		return core.ApplyResult{Revision: configuration.Revision, Code: "start_failed"}, errors.New("start_failed")
	}
	return core.ApplyResult{Revision: configuration.Revision, Success: true, Code: "success"}, nil
}
func (a *fakeAdapter) Rollback(context.Context, int64) error { return nil }
func (a *fakeAdapter) Health(context.Context) (core.HealthStatus, error) {
	return core.HealthStatus{Healthy: a.healthy}, nil
}
func (a *fakeAdapter) CollectStats(context.Context) (core.StatsBatch, error) {
	return core.StatsBatch{}, nil
}

func supervisorState(revision int64) desiredstate.State {
	return desiredstate.State{SchemaVersion: 1, Revision: revision, NodeID: "node-test", Logging: desiredstate.Logging{DomainOnly: true}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, Enabled: true, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
}

// compressSchedule shortens the supervisor's three-second schedule for a test.
func compressSchedule(t *testing.T, tick, base, maximum time.Duration) {
	t.Helper()
	previousTick, previousBase, previousMax := tickInterval, restoreRetryBase, restoreRetryMax
	tickInterval, restoreRetryBase, restoreRetryMax = tick, base, maximum
	t.Cleanup(func() { tickInterval, restoreRetryBase, restoreRetryMax = previousTick, previousBase, previousMax })
}

// committedManager applies one revision and returns the reconciler holding it.
func committedManager(t *testing.T, directory string, adapter *fakeAdapter) *reconcile.Reconciler {
	t.Helper()
	manager, err := reconcile.New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(context.Background(), supervisorState(1)); err != nil || !result.Success {
		t.Fatalf("initial apply: %+v %v", result, err)
	}
	return manager
}

// A restore that keeps failing the same way is retried with a doubling delay,
// and the identical failure is not logged on every attempt.
func TestRestoreFailuresBackOff(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	adapter := &fakeAdapter{}
	manager := committedManager(t, t.TempDir(), adapter)
	adapter.failApply = true
	var logs bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	Run(ctx, adapter, manager, slog.New(slog.NewTextHandler(&logs, nil)))
	attempts := adapter.applyCalls - 1 // the first apply is the committed revision
	if attempts < 3 {
		t.Fatalf("restore was not retried: %d attempts", attempts)
	}
	// A one-millisecond tick without the backoff would retry far more often.
	if attempts > 40 {
		t.Fatalf("restore retried without backoff: %d attempts", attempts)
	}
	if reported := strings.Count(logs.String(), "core recovery failed"); reported == 0 || reported >= attempts {
		t.Fatalf("failure logged %d times for %d attempts", reported, attempts)
	}
	// "retry_in" is the wait an operator can act on, not a nanosecond count.
	if !strings.Contains(logs.String(), "retry_in="+restoreRetryBase.String()) {
		t.Fatalf("no readable retry_in in: %s", logs.String())
	}
}

// A startup restore whose apply fails leaves the rendered digest empty, and the
// installed core stays unhealthy. The refresh used to run before the health check
// and applied the retained revision on every tick, so the restore backoff never
// took effect and the core was restarted every three seconds.
func TestStartupRestoreFailureIsNotRefreshed(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	ctx := context.Background()
	directory := t.TempDir()
	committedManager(t, directory, &fakeAdapter{})
	// The restarted Agent starts over with no rendered digest, and this core
	// neither starts the retained revision nor reports itself healthy.
	failing := &fakeAdapter{failApply: true}
	manager, err := reconcile.New(directory, "node-test", failing)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Restore(ctx); err == nil {
		t.Fatal("restore of a revision the core cannot start succeeded")
	}
	var logs bytes.Buffer
	runCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	Run(runCtx, failing, manager, slog.New(slog.NewTextHandler(&logs, nil)))
	attempts := failing.applyCalls
	if attempts < 3 {
		t.Fatalf("restore was not retried: %d attempts", attempts)
	}
	// A refresh on every tick would apply the revision a hundred times over.
	if attempts > 40 {
		t.Fatalf("refresh bypassed the restore backoff: %d apply attempts", attempts)
	}
}

// A retained revision the installed core cannot render is reported by the
// restore, never by a refresh on every tick while the core is unhealthy.
func TestUnhealthyCoreIsNotReconciled(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	adapter := &fakeAdapter{}
	manager := committedManager(t, t.TempDir(), adapter)
	adapter.failRender = true
	var logs bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	Run(ctx, adapter, manager, slog.New(slog.NewTextHandler(&logs, nil)))
	if strings.Contains(logs.String(), "approved runtime state reconciliation failed") {
		t.Fatalf("unhealthy core refreshed the runtime state: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "core recovery failed") {
		t.Fatalf("failed restore was not logged: %s", logs.String())
	}
	// Only the restores render while the core is unhealthy; a refresh per tick
	// would render a hundred times over.
	if adapter.renderCalls > 40 {
		t.Fatalf("unhealthy core rendered %d times", adapter.renderCalls)
	}
}

// A healthy core still gets the runtime refresh, and a refresh that changes the
// rendered configuration is applied once.
func TestHealthyCoreRefreshesRuntimeState(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	adapter := &fakeAdapter{}
	manager := committedManager(t, t.TempDir(), adapter)
	adapter.healthy = true
	adapter.renderVariant = "v2" // a time window that opened
	var logs bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	Run(ctx, adapter, manager, slog.New(slog.NewTextHandler(&logs, nil)))
	if !strings.Contains(logs.String(), "approved runtime state applied") {
		t.Fatalf("healthy core did not refresh the runtime state: %s", logs.String())
	}
	if adapter.applyCalls != 2 {
		t.Fatalf("refresh applied %d times, want 2", adapter.applyCalls)
	}
}

// A refresh that fails is logged once with its reason, and the refresh that
// succeeds afterwards reports the recovery.
func TestRefreshFailureIsLoggedOnceAndRecoveryReported(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	adapter := &fakeAdapter{}
	manager := committedManager(t, t.TempDir(), adapter)
	adapter.healthy = true
	adapter.failRefreshes = 3
	adapter.renderVariant = "v2"
	var logs bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	Run(ctx, adapter, manager, slog.New(slog.NewTextHandler(&logs, nil)))
	if failed := strings.Count(logs.String(), "approved runtime state reconciliation failed"); failed != 1 {
		t.Fatalf("refresh failure logged %d times: %s", failed, logs.String())
	}
	if recovered := strings.Count(logs.String(), "approved runtime state reconciliation recovered"); recovered != 1 {
		t.Fatalf("recovery logged %d times: %s", recovered, logs.String())
	}
	if !strings.Contains(logs.String(), "approved runtime state applied") {
		t.Fatalf("recovered refresh was not applied: %s", logs.String())
	}
}

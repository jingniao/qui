package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeAdapter runs no process: the retained revision renders, but every apply
// after the first one fails, the way a core that cannot start the revision does.
type fakeAdapter struct {
	healthy bool
	// healthErr is the reason the next health check fails; a real adapter
	// reports the branch it failed in.
	healthErr   error
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
	// healthCalls counts the health checks.
	healthCalls int
	// calls records the supervision calls in order, so a test can tell which
	// failure led to a render or an apply.
	calls []string
	// healOnApply recovers the scripted core on its next apply, so a test that
	// waits for a restore does not depend on how many ticks a run gets.
	healOnApply bool
}

func (a *fakeAdapter) Name() string { return "fake" }
func (a *fakeAdapter) DetectCapabilities(context.Context) (*pb.Capabilities, error) {
	return &pb.Capabilities{}, nil
}
func (a *fakeAdapter) Render(_ context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	a.calls = append(a.calls, "render")
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
	a.calls = append(a.calls, "apply")
	a.applyCalls++
	if a.failApply {
		return core.ApplyResult{Revision: configuration.Revision, Code: "start_failed"}, errors.New("start_failed")
	}
	if a.healOnApply {
		a.healthy, a.healthErr = true, nil
	}
	return core.ApplyResult{Revision: configuration.Revision, Success: true, Code: "success"}, nil
}
func (a *fakeAdapter) Rollback(context.Context, int64) error { return nil }
func (a *fakeAdapter) Health(context.Context) (core.HealthStatus, error) {
	a.calls = append(a.calls, "health")
	a.healthCalls++
	return core.HealthStatus{Healthy: a.healthy}, a.healthErr
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
	// The restore warning carries the health reason and the revision it
	// restored; an adapter that reports unhealthy without a reason still gets
	// one that names it.
	warning := `msg="core unhealthy; restoring last successful configuration" error="fake: unhealthy" revision=1`
	if !strings.Contains(logs.String(), warning) {
		t.Fatalf("restore warning has no reason: %s", logs.String())
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

// listenerFailure is the health failure a core listener that did not answer
// reports, the way both adapters wrap core.ErrListenerUnavailable.
func listenerFailure(endpoint string) error {
	return fmt.Errorf("core listener %s unavailable: %w: %w", endpoint, core.ErrListenerUnavailable, errors.New("dial tcp: i/o timeout"))
}

// callsStart reports whether the recorded supervision calls begin with want.
func callsStart(calls []string, want ...string) bool {
	return len(calls) >= len(want) && reflect.DeepEqual(calls[:len(want)], want)
}

// A listener that did not answer is not evidence that the core is broken, so the
// first two checks only defer the refresh; the third restores. The endpoints may
// differ per check: one core kept failing, so it is one streak.
func TestListenerFailureNeedsThreeConsecutiveChecks(t *testing.T) {
	var state coreRecovery
	listener := listenerFailure("127.0.0.1:1080")
	for check := 1; check < listenerFailureLimit; check++ {
		if state.observe(1, false, listener) {
			t.Fatalf("core restored after %d listener checks", check)
		}
	}
	if !state.observe(1, false, listener) {
		t.Fatal("third consecutive listener check did not restore the core")
	}
	var endpoints coreRecovery
	endpoints.observe(1, false, listenerFailure("127.0.0.1:1080"))
	endpoints.observe(1, false, listenerFailure("127.0.0.1:1081"))
	if !endpoints.observe(1, false, listenerFailure("127.0.0.1:1082")) {
		t.Fatal("three failing endpoints of one core were not one streak")
	}
}

// Every other unhealthy result restores at once, including one reported without a
// reason, and a healthy check starts the listener streak over.
func TestOtherHealthFailuresRestoreImmediately(t *testing.T) {
	for _, failure := range []error{fmt.Errorf("core exited: %w", errors.New("exit status 2")), errors.New("fake: unhealthy"), nil} {
		var state coreRecovery
		state.observe(1, false, listenerFailure("127.0.0.1:1080"))
		state.observe(1, false, listenerFailure("127.0.0.1:1080"))
		if !state.observe(1, false, failure) {
			t.Fatalf("%v waited for three listener failures", failure)
		}
	}
	var state coreRecovery
	state.observe(1, false, listenerFailure("127.0.0.1:1080"))
	state.observe(1, false, listenerFailure("127.0.0.1:1080"))
	state.observe(1, true, nil)
	if state.observe(1, false, listenerFailure("127.0.0.1:1080")) {
		t.Fatal("a healthy check did not reset the listener streak")
	}
}

// A new revision, a component update and a successful restore all end the run of
// listener failures a streak counted.
func TestRevisionUpdateAndRestoreResetTheListenerStreak(t *testing.T) {
	listener := listenerFailure("127.0.0.1:1080")
	var state coreRecovery
	state.observe(1, false, listener)
	state.observe(1, false, listener)
	if state.observe(2, false, listener) {
		t.Fatal("the revision change kept the listener streak")
	}
	state.observe(2, false, listener)
	state.reset() // a component update owned the core
	if state.observe(2, false, listener) {
		t.Fatal("a held core kept the listener streak")
	}
	state.observe(2, false, listener)
	state.reset() // the restore succeeded
	if state.observe(2, false, listener) {
		t.Fatal("a successful restore kept the listener streak")
	}
}

// One failed listener check of a working core neither restarts it nor refreshes
// the runtime state; the third restores it.
func TestDeferredListenerFailureDoesNotRefresh(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	adapter := &fakeAdapter{}
	manager := committedManager(t, t.TempDir(), adapter)
	adapter.calls, adapter.healthErr, adapter.healOnApply = nil, listenerFailure("127.0.0.1:1080"), true
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	Run(ctx, adapter, manager, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	// Two deferred checks, then the restore renders and applies the revision
	// once: no refresh ran while the core was still serving.
	if want := []string{"health", "health", "health", "render", "apply"}; !callsStart(adapter.calls, want...) {
		t.Fatalf("calls: %v", adapter.calls)
	}
}

// A restore that fails keeps the listener threshold, so the retries that follow
// keep the backoff instead of waiting for three fresh failures.
func TestFailedRestoreKeepsTheListenerThreshold(t *testing.T) {
	var state coreRecovery
	now := time.Now()
	listener := listenerFailure("127.0.0.1:1080")
	for check := 1; check < listenerFailureLimit; check++ {
		state.observe(1, false, listener)
	}
	if !state.observe(1, false, listener) || !state.backoff.ready(1, now) {
		t.Fatal("the third listener check was not eligible for a restore")
	}
	state.backoff.failed(errors.New("start failed"), now)
	if !state.observe(1, false, listener) {
		t.Fatal("a failed restore waited for three fresh listener failures")
	}
	if state.backoff.ready(1, now) {
		t.Fatal("the retry ignored the restore backoff")
	}
	if !state.backoff.ready(1, now.Add(state.backoff.delay)) {
		t.Fatal("the retry never became due")
	}
}

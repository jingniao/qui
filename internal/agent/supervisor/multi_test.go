package supervisor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
)

type multiTestAdapter struct {
	fakeAdapter
	name string
}

func (a *multiTestAdapter) Name() string                                { return a.name }
func (a *multiTestAdapter) ConfigureRuntime(core.RuntimeHooks)          {}
func (a *multiTestAdapter) Close() error                                { return nil }
func (a *multiTestAdapter) SampleTraffic(context.Context) error         { return nil }
func (a *multiTestAdapter) SupportsAccountTelemetry() bool              { return false }
func (a *multiTestAdapter) OutboundHealth() map[string]scheduler.Health { return nil }

func TestMultiSupervisorRestoresOnlyFailedCoreWithBackoff(t *testing.T) {
	compressSchedule(t, 10*time.Millisecond, 40*time.Millisecond, 80*time.Millisecond)
	a := &multiTestAdapter{name: "xray", fakeAdapter: fakeAdapter{healthy: true}}
	b := &multiTestAdapter{name: "sing-box", fakeAdapter: fakeAdapter{healthy: true}}
	multi, err := core.NewMultiRuntime(a, b)
	if err != nil {
		t.Fatal(err)
	}
	m, err := reconcile.New(t.TempDir(), "node-test", multi)
	if err != nil {
		t.Fatal(err)
	}
	s := supervisorState(1)
	s.Inbounds = append(s.Inbounds, desiredstate.Inbound{ID: "other", Core: "sing-box", Protocol: "socks5", Listen: "127.0.0.1", Port: 1081, Enabled: true, DefaultOutbound: "direct"})
	if _, err := m.Apply(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	b.healthy = false
	b.failApply = true
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	Run(ctx, multi, m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if a.applyCalls != 1 {
		t.Fatalf("healthy core restarted %d times", a.applyCalls)
	}
	if b.applyCalls < 2 || b.applyCalls > 4 {
		t.Fatalf("failed core ignored backoff: %d", b.applyCalls)
	}
}

// A per-core restore logs the core, the reason its health check failed and the
// revision it restored. A listener that did not answer reaches the restore on the
// third consecutive check.
func TestMultiSupervisorLogsWhyACoreWasRestored(t *testing.T) {
	compressSchedule(t, 10*time.Millisecond, 40*time.Millisecond, 80*time.Millisecond)
	a := &multiTestAdapter{name: "xray", fakeAdapter: fakeAdapter{healthy: true}}
	b := &multiTestAdapter{name: "sing-box", fakeAdapter: fakeAdapter{healthy: false, healthErr: fmt.Errorf("sing-box listener health failed: %w", core.ErrListenerUnavailable)}}
	multi, err := core.NewMultiRuntime(a, b)
	if err != nil {
		t.Fatal(err)
	}
	m, err := reconcile.New(t.TempDir(), "node-test", multi)
	if err != nil {
		t.Fatal(err)
	}
	s := supervisorState(1)
	s.Inbounds = append(s.Inbounds, desiredstate.Inbound{ID: "other", Core: "sing-box", Protocol: "socks5", Listen: "127.0.0.1", Port: 1081, Enabled: true, DefaultOutbound: "direct"})
	if _, err := m.Apply(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	Run(ctx, multi, m, slog.New(slog.NewTextHandler(&logs, nil)))
	want := `msg="core unhealthy; restored last successful configuration" core=sing-box error="sing-box listener health failed: core listener did not answer" revision=1`
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("restored core logged no reason: %s", logs.String())
	}
}

// A listener-only failure of one core also waits for its third check, and the
// healthy peer is never restored.
func TestMultiListenerFailureRestoresOnlyTheFailingCore(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	a := &multiTestAdapter{name: "xray", fakeAdapter: fakeAdapter{healthy: true}}
	b := &multiTestAdapter{name: "sing-box", fakeAdapter: fakeAdapter{healthy: true}}
	multi, err := core.NewMultiRuntime(a, b)
	if err != nil {
		t.Fatal(err)
	}
	m, err := reconcile.New(t.TempDir(), "node-test", multi)
	if err != nil {
		t.Fatal(err)
	}
	s := supervisorState(1)
	s.Inbounds = append(s.Inbounds, desiredstate.Inbound{ID: "other", Core: "sing-box", Protocol: "socks5", Listen: "127.0.0.1", Port: 1081, Enabled: true, DefaultOutbound: "direct"})
	if _, err := m.Apply(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	a.calls, b.calls = nil, nil
	b.healthy, b.healthErr, b.healOnApply = false, fmt.Errorf("sing-box listener health failed: %w", core.ErrListenerUnavailable), true
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	Run(ctx, multi, m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if want := []string{"health", "health", "health", "render", "apply"}; !callsStart(b.calls, want...) {
		t.Fatalf("failing core calls: %v", b.calls)
	}
	for _, call := range a.calls {
		if call == "apply" {
			t.Fatalf("healthy core was restored: %v", a.calls)
		}
	}
}

// binaryMultiAdapter is a supervision fake that can also be the fixed executable
// target of a core update, the way a real adapter is, so a test can take the
// component hold that update writes.
type binaryMultiAdapter struct {
	multiTestAdapter
	executable string
}

func (a *binaryMultiAdapter) ExecutablePath() string { return a.executable }
func (a *binaryMultiAdapter) DetectVersion(context.Context, string) (string, error) {
	return "", nil
}
func (a *binaryMultiAdapter) ValidateExecutable(context.Context, string, core.RenderedConfig) (string, error) {
	return "", nil
}
func (a *binaryMultiAdapter) SelectExecutable(context.Context, string) error { return nil }

var _ core.BinaryRuntime = (*binaryMultiAdapter)(nil)

// countingCores counts the core samples one supervisor tick asks for.
type countingCores struct {
	*core.MultiRuntime
	samples int
}

func (c *countingCores) CoreHealths(ctx context.Context) []core.CoreHealth {
	c.samples++
	return c.MultiRuntime.CoreHealths(ctx)
}

// A core that is being added is held before the runtime reports it, so a hold can
// exist that no reported core owns. The refresh refuses every hold, and asking it
// anyway logged that refusal at Error on every tick while the core was installed.
func TestMultiHealthyCoresDoNotRefreshUnderTheHoldOfAnUnreportedCore(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	ctx := context.Background()
	directory := t.TempDir()
	// The node that installed sing-box reports both cores.
	xray := &binaryMultiAdapter{multiTestAdapter: multiTestAdapter{name: "xray", fakeAdapter: fakeAdapter{healthy: true}}, executable: "xray"}
	singbox := &binaryMultiAdapter{multiTestAdapter: multiTestAdapter{name: "sing-box", fakeAdapter: fakeAdapter{healthy: true}}, executable: "sing-box"}
	installed, err := core.NewMultiRuntime(xray, singbox)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := reconcile.New(directory, "node-test", installed)
	if err != nil {
		t.Fatal(err)
	}
	s := supervisorState(1)
	s.Inbounds = append(s.Inbounds, desiredstate.Inbound{ID: "other", Core: "sing-box", Protocol: "socks5", Listen: "127.0.0.1", Port: 1081, Enabled: true, DefaultOutbound: "direct"})
	if _, err := manager.Apply(ctx, s); err != nil {
		t.Fatal(err)
	}
	// The update holds sing-box before the core can be selected.
	if err := manager.WithCoreComponentUpdate(ctx, 1, "sing-box", func(lease *reconcile.CoreUpdateLease) error { return lease.Hold() }); err != nil {
		t.Fatal(err)
	}
	// The Agent of the same node, still reporting only xray.
	reporting := &multiTestAdapter{name: "xray", fakeAdapter: fakeAdapter{healthy: true}}
	multi, err := core.NewMultiRuntime(reporting)
	if err != nil {
		t.Fatal(err)
	}
	adding := &countingCores{MultiRuntime: multi}
	held, err := reconcile.New(directory, "node-test", adding)
	if err != nil {
		t.Fatal(err)
	}
	if !held.CoreUpdateHeld() || held.CoreComponentHeld("xray") {
		t.Fatal("the test needs a hold no reported core owns")
	}
	var logs bytes.Buffer
	runCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	Run(runCtx, adding, held, slog.New(slog.NewTextHandler(&logs, nil)))
	if adding.samples == 0 {
		t.Fatal("the tick never sampled the reported core")
	}
	if reporting.renderCalls != 0 || reporting.applyCalls != 0 {
		t.Fatalf("the held runtime was refreshed: %d renders, %d applies", reporting.renderCalls, reporting.applyCalls)
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("a held refresh was logged as a failure: %s", logs.String())
	}
}

// countingMulti counts the aggregate health samples of a multi-core runtime.
type countingMulti struct {
	*core.MultiRuntime
	aggregate int
}

func (c *countingMulti) Health(ctx context.Context) (core.HealthStatus, error) {
	c.aggregate++
	return c.MultiRuntime.Health(ctx)
}

// A healthy multi-core node is sampled once per core and tick: a second,
// aggregate sample could restore every core from a stale answer.
func TestMultiTickSamplesEachCoreHealthOnce(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	a := &multiTestAdapter{name: "xray", fakeAdapter: fakeAdapter{healthy: true}}
	b := &multiTestAdapter{name: "sing-box", fakeAdapter: fakeAdapter{healthy: true}}
	multi, err := core.NewMultiRuntime(a, b)
	if err != nil {
		t.Fatal(err)
	}
	tracked := &countingMulti{MultiRuntime: multi}
	m, err := reconcile.New(t.TempDir(), "node-test", tracked)
	if err != nil {
		t.Fatal(err)
	}
	s := supervisorState(1)
	s.Inbounds = append(s.Inbounds, desiredstate.Inbound{ID: "other", Core: "sing-box", Protocol: "socks5", Listen: "127.0.0.1", Port: 1081, Enabled: true, DefaultOutbound: "direct"})
	if _, err := m.Apply(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	Run(ctx, tracked, m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if tracked.aggregate != 0 {
		t.Fatalf("aggregate health sampled %d times", tracked.aggregate)
	}
	if a.healthCalls == 0 || a.healthCalls != b.healthCalls {
		t.Fatalf("core checks: xray %d, sing-box %d", a.healthCalls, b.healthCalls)
	}
}

// togglingCores reports a failing core on every other tick, so a core that is not
// reported in between must not keep the listener streak it had.
type togglingCores struct {
	fakeAdapter
	checks   int
	restores int
}

func (t *togglingCores) CoreHealths(context.Context) []core.CoreHealth {
	t.checks++
	if t.checks%2 == 0 {
		return nil
	}
	return []core.CoreHealth{{Status: &pb.CoreStatus{Name: "sing-box", Healthy: false, Error: "core_unhealthy"}, Err: listenerFailure("127.0.0.1:1081")}}
}
func (t *togglingCores) RestoreCore(context.Context, string, desiredstate.State) error {
	t.restores++
	return nil
}

// A core that is no longer reported starts its listener streak over when it comes
// back.
func TestUnreportedCoreDoesNotKeepItsListenerStreak(t *testing.T) {
	compressSchedule(t, time.Millisecond, 2*time.Millisecond, 20*time.Millisecond)
	adapter := &togglingCores{}
	m, err := reconcile.New(t.TempDir(), "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(context.Background(), supervisorState(1)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	Run(ctx, adapter, m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if adapter.restores != 0 {
		t.Fatalf("an unreported core kept its listener streak: %d restores", adapter.restores)
	}
	if adapter.checks < 6 {
		t.Fatalf("the core was reported %d times", adapter.checks)
	}
}

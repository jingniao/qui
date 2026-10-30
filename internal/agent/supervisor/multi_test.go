package supervisor

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
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

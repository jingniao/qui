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
	healthy    bool
	failApply  bool
	applyCalls int
}

func (a *fakeAdapter) Name() string { return "fake" }
func (a *fakeAdapter) DetectCapabilities(context.Context) (*pb.Capabilities, error) {
	return &pb.Capabilities{}, nil
}
func (a *fakeAdapter) Render(_ context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	data, err := json.Marshal(state)
	return core.RenderedConfig{Revision: state.Revision, Data: data}, err
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

// A restore that keeps failing the same way is retried with a doubling delay,
// and the identical failure is not logged on every attempt.
func TestRestoreFailuresBackOff(t *testing.T) {
	previousTick, previousBase, previousMax := tickInterval, restoreRetryBase, restoreRetryMax
	tickInterval, restoreRetryBase, restoreRetryMax = time.Millisecond, 2*time.Millisecond, 20*time.Millisecond
	defer func() { tickInterval, restoreRetryBase, restoreRetryMax = previousTick, previousBase, previousMax }()
	adapter := &fakeAdapter{}
	manager, err := reconcile.New(t.TempDir(), "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(context.Background(), supervisorState(1)); err != nil || !result.Success {
		t.Fatalf("initial apply: %+v %v", result, err)
	}
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
}

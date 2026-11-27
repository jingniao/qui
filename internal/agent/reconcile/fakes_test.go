package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"proxycontrol/internal/agent/core"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
)

// fakeCoreAdapter is a core.Adapter that never runs a process. Tests use it to
// exercise Reconciler retry and scheduled-refresh logic without a real core.
type fakeCoreAdapter struct {
	renderCalls        int
	validateCalls      int
	applyCalls         int
	failRender         bool
	validationErr      error
	applyValidationErr error
	// failRenderRevision fails only that revision's render, the way a core the
	// operator installed cannot start a retained revision built for another one.
	failRenderRevision int64
	// failHealth keeps Health unhealthy even after a successful apply, so a
	// re-apply of the retained revision does not take the early exit.
	failHealth    bool
	renderVariant string
	applyCodes    []string // one per Apply call; an empty entry means success
	closed        bool
	closeErr      error
	applied       []core.RenderedConfig
}

func (a *fakeCoreAdapter) Name() string { return "fake" }
func (a *fakeCoreAdapter) DetectCapabilities(context.Context) (*pb.Capabilities, error) {
	return &pb.Capabilities{}, nil
}
func (a *fakeCoreAdapter) Render(_ context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	a.renderCalls++
	if a.failRender || a.failRenderRevision != 0 && state.Revision == a.failRenderRevision {
		return core.RenderedConfig{}, errors.New("render failed")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	data = append(data, a.renderVariant...)
	return core.RenderedConfig{Revision: state.Revision, Data: data}, nil
}
func (a *fakeCoreAdapter) Validate(context.Context, core.RenderedConfig) error {
	a.validateCalls++
	return a.validationErr
}
func (a *fakeCoreAdapter) Apply(_ context.Context, configuration core.RenderedConfig) (core.ApplyResult, error) {
	if a.applyValidationErr != nil {
		return core.ApplyResult{Revision: configuration.Revision, Code: "validation_failed"}, a.applyValidationErr
	}
	code := ""
	if a.applyCalls < len(a.applyCodes) {
		code = a.applyCodes[a.applyCalls]
	}
	a.applyCalls++
	if code != "" {
		return core.ApplyResult{Revision: configuration.Revision, Code: code}, errors.New(code)
	}
	a.applied = append(a.applied, configuration)
	return core.ApplyResult{Revision: configuration.Revision, Success: true, Code: "success"}, nil
}
func (a *fakeCoreAdapter) Rollback(context.Context, int64) error { return nil }
func (a *fakeCoreAdapter) Health(context.Context) (core.HealthStatus, error) {
	return core.HealthStatus{Healthy: !a.failHealth && len(a.applied) > 0}, nil
}
func (a *fakeCoreAdapter) CollectStats(context.Context) (core.StatsBatch, error) {
	return core.StatsBatch{}, nil
}
func (a *fakeCoreAdapter) Close() error { a.closed = true; return a.closeErr }

type fakeXrayRuntime struct{ fakeCoreAdapter }

func (*fakeXrayRuntime) Name() string                                { return "xray" }
func (*fakeXrayRuntime) ConfigureRuntime(core.RuntimeHooks)          {}
func (*fakeXrayRuntime) SampleTraffic(context.Context) error         { return nil }
func (*fakeXrayRuntime) SupportsAccountTelemetry() bool              { return false }
func (*fakeXrayRuntime) OutboundHealth() map[string]scheduler.Health { return nil }

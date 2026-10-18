package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"proxycontrol/internal/agent/core"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// fakeCoreAdapter is a core.Adapter that never runs a process. Tests use it to
// exercise Reconciler retry and scheduled-refresh logic without a real core.
type fakeCoreAdapter struct {
	renderCalls   int
	validateCalls int
	applyCalls    int
	failRender    bool
	failValidate  bool
	renderVariant string
	applyCodes    []string // one per Apply call; an empty entry means success
	closed        bool
	applied       []core.RenderedConfig
}

func (a *fakeCoreAdapter) Name() string { return "fake" }
func (a *fakeCoreAdapter) DetectCapabilities(context.Context) (*pb.Capabilities, error) {
	return &pb.Capabilities{}, nil
}
func (a *fakeCoreAdapter) Render(_ context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	a.renderCalls++
	if a.failRender {
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
	if a.failValidate {
		return errors.New("validation failed")
	}
	return nil
}
func (a *fakeCoreAdapter) Apply(_ context.Context, configuration core.RenderedConfig) (core.ApplyResult, error) {
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
	return core.HealthStatus{Healthy: len(a.applied) > 0}, nil
}
func (a *fakeCoreAdapter) CollectStats(context.Context) (core.StatsBatch, error) {
	return core.StatsBatch{}, nil
}
func (a *fakeCoreAdapter) Close() error { a.closed = true; return nil }

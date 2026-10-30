package reconcile

import (
	"context"
	"errors"
	"os/exec"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// CoreUpdateLease cannot be retained for use after WithCoreUpdate returns. All
// fixed operations remain serialized with the current immutable configuration.
type CoreUpdateLease struct {
	reconciler *Reconciler
	runtime    core.BinaryRuntime
	active     bool
	component  string
}

func (r *Reconciler) WithCoreUpdate(ctx context.Context, expectedRevision int64, operation func(*CoreUpdateLease) error) error {
	return r.WithCoreComponentUpdate(ctx, expectedRevision, "", operation)
}
func (r *Reconciler) WithCoreComponentUpdate(ctx context.Context, expectedRevision int64, component string, operation func(*CoreUpdateLease) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.controlOnly {
		return core.ErrReleaseRecovery
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if expectedRevision < 1 || r.manifest.Revision != expectedRevision || operation == nil {
		return errors.New("core update baseline no longer matches current revision")
	}
	if err := r.verifyRequiredSuccessReceipt(r.manifest); err != nil {
		return err
	}
	var adapter core.Adapter = r.Adapter
	if multi, ok := adapter.(*core.MultiRuntime); ok {
		adapter = multi.Component(component)
		if adapter == nil {
			return errors.New("unknown update component")
		}
		component = adapter.Name()
	} else if component != "" && component != adapter.Name() {
		return errors.New("unknown update component")
	} else {
		component = ""
	}
	runtime, ok := adapter.(core.BinaryRuntime)
	if !ok {
		return errors.New("core adapter does not support fixed executable replacement")
	}
	lease := &CoreUpdateLease{reconciler: r, runtime: runtime, component: component, active: true}
	defer func() { lease.active = false }()
	return operation(lease)
}
func (l *CoreUpdateLease) Component() string { return l.runtime.Name() }
func (l *CoreUpdateLease) NodeID() string    { return l.reconciler.NodeID }
func (l *CoreUpdateLease) Revision() int64 {
	if !l.active {
		return 0
	}
	return l.reconciler.manifest.Revision
}
func (l *CoreUpdateLease) Hold() error {
	if l.active {
		if l.component != "" {
			return l.reconciler.holdComponent(l.component)
		}
		return l.reconciler.holdCoreUpdate()
	}
	return core.ErrReleaseRecovery
}
func (l *CoreUpdateLease) Release() error {
	if l.active {
		if l.component != "" {
			return l.reconciler.releaseComponent(l.component)
		}
		return l.reconciler.releaseCoreUpdate()
	}
	return core.ErrReleaseRecovery
}
func (l *CoreUpdateLease) Current(ctx context.Context) (string, string, error) {
	if !l.active {
		return "", "", errors.New("core update lease expired")
	}
	capabilities, err := l.runtime.DetectCapabilities(ctx)
	if err != nil {
		return "", "", err
	}
	path, err := exec.LookPath(l.runtime.ExecutablePath())
	return path, capabilities.CoreVersion, err
}

// DetectVersion runs the candidate executable's own version command, before
// anything is switched in. The requested version must match it exactly.
func (l *CoreUpdateLease) DetectVersion(ctx context.Context, path string) (string, error) {
	if !l.active {
		return "", errors.New("core update lease expired")
	}
	return l.runtime.DetectVersion(ctx, path)
}
func (l *CoreUpdateLease) Check(ctx context.Context, path, expectedVersion string) error {
	if !l.active {
		return errors.New("core update lease expired")
	}
	r := l.reconciler
	data, err := r.readRevision(r.manifest.Revision)
	if err != nil || digest(data) != r.manifest.Digest {
		return errors.New("core update desired configuration integrity failed")
	}
	state, err := desiredstate.Decode(data)
	if err != nil || state.NodeID != r.NodeID {
		return errors.New("core update desired configuration identity failed")
	}
	adapter := r.Adapter
	if l.component != "" {
		state = state.ForCore(l.component, r.Adapter.Name())
		adapter = l.runtime
	}
	if l.component != "" {
		active := false
		for _, in := range state.Inbounds {
			active = active || in.Enabled
		}
		if !active {
			version, err := l.runtime.DetectVersion(ctx, path)
			if err != nil {
				return err
			}
			if version != expectedVersion {
				return errors.New("core version mismatch")
			}
			return nil
		}
	}
	configuration, err := adapter.Render(ctx, state)
	if err != nil {
		return err
	}
	version, err := l.runtime.ValidateExecutable(ctx, path, configuration)
	if err != nil {
		return err
	}
	if version != expectedVersion {
		return errors.New("signed release version does not match native core version")
	}
	return nil
}
func (l *CoreUpdateLease) Activate(ctx context.Context, path string) error {
	if !l.active {
		return errors.New("core update lease expired")
	}
	if l.reconciler.coreUpdateHoldInvalid || l.reconciler.componentHoldsInvalid[l.component] {
		return core.ErrReleaseRecovery
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := l.runtime.Close(); err != nil {
		return err
	}
	if err := l.runtime.SelectExecutable(ctx, path); err != nil {
		return err
	}
	if l.component != "" {
		if err := l.reconciler.restoreCoreLocked(ctx, l.component); err != nil {
			return err
		}
		if multi, ok := l.reconciler.Adapter.(*core.MultiRuntime); ok && !multi.CoreActive(l.component) {
			return nil
		}
	} else if err := l.reconciler.restoreRevision(ctx, l.reconciler.manifest.Revision); err != nil {
		return err
	}
	health, err := l.runtime.Health(ctx)
	if err != nil {
		return err
	}
	if !health.Healthy {
		return errors.New("updated core health check failed")
	}
	return nil
}
func (l *CoreUpdateLease) Stop(context.Context) error {
	if !l.active {
		return errors.New("core update lease expired")
	}
	return l.runtime.Close()
}

// UnconfirmedStop reports a core stop that could not confirm the exact child's
// exit. The caller must not write a closed terminal state or start a replacement.
func (l *CoreUpdateLease) UnconfirmedStop() bool {
	if !l.active {
		return false
	}
	observer, ok := l.runtime.(interface{ UnconfirmedStop() bool })
	return ok && observer.UnconfirmedStop()
}

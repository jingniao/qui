package reconcile

import (
	"context"
	"errors"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Refresh applies only runtime changes implied by the approved immutable snapshot.
func (r *Reconciler) Refresh(ctx context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.coreUpdateHold || r.controlOnly {
		return false, core.ErrReleaseRecovery
	}
	if r.manifest.Revision == 0 {
		return false, nil
	}
	data, err := r.readRevision(r.manifest.Revision)
	if err != nil {
		return false, err
	}
	if digest(data) != r.manifest.Digest {
		return false, errors.New("current revision checksum mismatch")
	}
	state, err := desiredstate.Decode(data)
	if err != nil {
		return false, err
	}
	if state.NodeID != r.NodeID {
		return false, errors.New("saved revision identity mismatch")
	}
	// A time-window re-render or apply that fails must not disturb the running
	// revision or core: keep serving the last good configuration and retry on
	// the next tick. Never fall back to block-all and never stop the core.
	rendered, err := r.Adapter.Render(ctx, state)
	if err != nil {
		return false, err
	}
	if digest(rendered.Data) == r.renderedDigest {
		return false, nil
	}
	if _, err := r.Adapter.Apply(ctx, rendered); err != nil {
		return false, err
	}
	r.renderedDigest = digest(rendered.Data)
	return true, nil
}

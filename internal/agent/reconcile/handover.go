package reconcile

import (
	"context"
	"errors"
	"fmt"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// BlockStartup keeps a failed handover online for control, but never permits a
// publisher, binary installer or supervisor to turn it into a restart.
func (r *Reconciler) BlockStartup(err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if multi, ok := r.Adapter.(*core.MultiRuntime); ok {
		err = errors.Join(err, multi.ReleaseHandoverOwnership())
		multi.BlockHandover(err)
	}
	r.startupErr = err
	return err
}

func (r *Reconciler) StartupError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startupErr
}

// RestoreStartup consumes only authenticated successful DesiredState. Ordinary
// Restore and Rollback keep their restart semantics.
func (r *Reconciler) RestoreStartup(ctx context.Context, materials map[string]core.RuntimeMaterial) (resultErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		if resultErr != nil {
			r.startupErr = resultErr
		}
	}()
	if r.anyCoreHold() || r.controlOnly {
		return core.ErrReleaseRecovery
	}
	if r.manifest.Revision == 0 {
		if len(materials) == 0 {
			return r.applyHostNetwork(ctx, desiredstate.State{})
		}
		return errors.Join(core.ErrHandoverInvalid, errors.New("handover has no successful DesiredState"))
	}
	if err := r.verifyRequiredSuccessReceipt(r.manifest); err != nil {
		return err
	}
	data, err := r.readRevision(r.manifest.Revision)
	if err != nil {
		return err
	}
	if digest(data) != r.manifest.Digest {
		return errors.New("current revision checksum mismatch")
	}
	state, err := desiredstate.Decode(data)
	if err != nil {
		return err
	}
	if state.NodeID != r.NodeID || state.Revision != r.manifest.Revision {
		return core.ErrHandoverConflict
	}
	if err := r.verifyOutboundNetworks(state); err != nil {
		return err
	}
	multi, ok := r.Adapter.(*core.MultiRuntime)
	if !ok {
		return core.ErrHandoverUnsupported
	}
	// Do not download/activate rule sets here: live cores still reference their
	// files. Native validation and the installed dependency digests are checked
	// against the retained material before any core can be stopped.
	rendered, changed, err := multi.RestoreStartup(ctx, state, materials)
	if err != nil {
		return fmt.Errorf("startup handover: %w", err)
	}
	hostNetworkApplied := state.NeedsForcedEgress()
	if hostNetworkApplied {
		if err := r.applyHostNetwork(ctx, state); err != nil {
			return err
		}
	}
	if changed {
		if _, err := multi.Apply(ctx, rendered); err != nil {
			return err
		}
	}
	if err := multi.FinishStartupRuntimes(); err != nil {
		return err
	}
	if !hostNetworkApplied {
		if err := r.applyHostNetwork(ctx, state); err != nil {
			return err
		}
	}
	r.renderedDigest = digest(rendered.Data)
	return nil
}

package reconcile

import (
	"context"
	"errors"
	"proxycontrol/pkg/contracts/desiredstate"
)

// RestoreCore serializes targeted supervision with publication and verifies the
// same immutable snapshot and receipt as a full restore.
func (r *Reconciler) RestoreCore(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.coreUpdateHold || r.componentHolds[name] || r.controlOnly {
		return errors.New("core release recovery required")
	}
	return r.restoreCoreLocked(ctx, name)
}
func (r *Reconciler) restoreCoreLocked(ctx context.Context, name string) error {
	target, ok := r.Adapter.(interface {
		RestoreCore(context.Context, string, desiredstate.State) error
	})
	if !ok {
		return errors.New("targeted core restore unsupported")
	}
	data, err := r.readRevision(r.manifest.Revision)
	if err != nil {
		return err
	}
	if digest(data) != r.manifest.Digest {
		return errors.New("current revision checksum mismatch")
	}
	if err := r.verifyRequiredSuccessReceipt(r.manifest); err != nil {
		return err
	}
	s, err := desiredstate.Decode(data)
	if err != nil {
		return err
	}
	if s.NodeID != r.NodeID || s.Revision != r.manifest.Revision {
		return errors.New("saved revision identity mismatch")
	}
	return target.RestoreCore(ctx, name, s)
}

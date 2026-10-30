package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
)

// WithCoreMembership serializes inventory publication with apply, refresh,
// rollback, updates and recovery. Apply has no unlocked pending revision: a
// waiting Apply is revalidated against the changed runtime before it can switch.
func (r *Reconciler) WithCoreMembership(ctx context.Context, revision int64, component string, operation func(*core.MultiRuntime) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.controlOnly || r.anyCoreHold() {
		return core.ErrReleaseRecovery
	}
	if revision < 1 || revision != r.manifest.Revision {
		return errors.New("revision_mismatch")
	}
	if err := r.verifyRequiredSuccessReceipt(r.manifest); err != nil {
		return err
	}
	multi, ok := r.Adapter.(*core.MultiRuntime)
	if !ok {
		return errors.New("multi-core unavailable")
	}
	data, err := r.readRevision(revision)
	if err != nil {
		return err
	}
	if digest(data) != r.manifest.Digest {
		return errors.New("revision checksum mismatch")
	}
	s, err := desiredstate.Decode(data)
	if err != nil {
		return err
	}
	if s.NodeID != r.NodeID {
		return errors.New("revision identity mismatch")
	}
	for _, in := range s.Inbounds {
		n := in.Core
		if n == "" {
			n = multi.Name()
		}
		if n == component {
			return errors.New("core_in_use")
		}
	}
	// An interrupted Apply may have written its authenticated snapshots before
	// committing current.json. Do not strand a staged revision's core on disk.
	entries, err := os.ReadDir(filepath.Join(r.Directory, "revisions"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id, e := strconv.ParseInt(entry.Name(), 10, 64)
		if e != nil || id < 1 || id == revision {
			continue
		}
		retained := false
		for _, n := range r.manifest.Successful {
			retained = retained || n == id
		}
		if retained {
			continue
		}
		data, e := r.readRevision(id)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		state, e := desiredstate.Decode(data)
		if e != nil {
			return e
		}
		if state.NodeID != r.NodeID || state.Revision != id {
			return errors.New("pending revision identity mismatch")
		}
		if result, e := os.ReadFile(filepath.Join(r.Directory, "revisions", entry.Name(), "result.json")); e == nil {
			var receipt Receipt
			if json.Unmarshal(result, &receipt) != nil || receipt.Digest != digest(data) {
				return errors.New("pending revision receipt mismatch")
			}
			if !receipt.Result.Success {
				continue
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		for _, in := range state.Inbounds {
			n := in.Core
			if n == "" {
				n = multi.Name()
			}
			if n == component {
				return errors.New("core_pending")
			}
		}
	}
	return operation(multi)
}

// Inventory receipts share the same serialization boundary as membership.
func (r *Reconciler) WithCoreInventory(operation func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return operation()
}

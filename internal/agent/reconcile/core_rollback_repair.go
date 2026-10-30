package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"proxycontrol/internal/agent/core"
)

// RepairCoreRollback finishes an interrupted core switch at startup: the
// runtime is pointed back at the retained image, the current revision is
// restored and the hold is released. An unreadable hold marker is preserved as
// evidence first, so a restart never leaves the node permanently held.
func (r *Reconciler) RepairCoreRollback(ctx context.Context, path string, components ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.controlOnly {
		return core.ErrReleaseRecovery
	}
	component := ""
	if len(components) > 0 {
		component = components[0]
	}
	var adapter core.Adapter = r.Adapter
	if multi, ok := adapter.(*core.MultiRuntime); ok {
		adapter = multi.Component(component)
		if adapter == nil {
			return errors.New("unknown repair component")
		}
		component = adapter.Name()
	} else {
		component = ""
	}
	runtime, ok := adapter.(core.BinaryRuntime)
	if !ok {
		return errors.New("core adapter does not support fixed executable replacement")
	}
	if path == "" {
		return core.ErrReleaseRecovery
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if component != "" && r.componentHoldsInvalid[component] {
		if err := preserveDamagedHold(r.componentHoldPath(component)); err != nil {
			return err
		}
		r.componentHoldsInvalid[component] = false
	}
	if r.coreUpdateHoldInvalid {
		if err := preserveDamagedHold(filepath.Join(r.Directory, releaseHoldName)); err != nil {
			return err
		}
		r.coreUpdateHoldInvalid = false
	}
	if err := runtime.Close(); err != nil {
		return err
	}
	if err := runtime.SelectExecutable(ctx, path); err != nil {
		return err
	}
	if r.manifest.Revision > 0 {
		var err error
		if component != "" {
			err = r.restoreCoreLocked(ctx, component)
		} else {
			err = r.restoreRevision(ctx, r.manifest.Revision)
		}
		if err != nil {
			return err
		}
	}
	if component != "" {
		if err := r.releaseComponent(component); err != nil {
			return err
		}
	}
	return r.releaseCoreUpdate()
}

// preserveDamagedHold keeps the exact bytes of an unreadable marker as
// evidence next to the hold directory.
func preserveDamagedHold(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	directory := filepath.Join(filepath.Dir(path), "release-hold-recovery")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	return writeFile(filepath.Join(directory, filepath.Base(path)+".damaged"), data)
}

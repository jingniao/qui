package xray

import (
	"errors"
	"proxycontrol/internal/agent/core"
)

// runMarker binds the shared core-run marker material to this adapter: its runtime
// directory, the node identity it starts children for and its own core name. The
// binding is resolved on use because adapters are plain values built by the node
// wiring and by tests, never by a constructor.
func (a *Adapter) runMarker() *core.RunMarker {
	a.marker.Directory, a.marker.NodeID, a.marker.Core = a.Directory, a.NodeID, a.Name()
	return &a.marker
}

// runningChildLocked reports the child this adapter holds as the running core: the
// durable marker must also name the epoch the statistics are attributed to, so a
// start that never settled keeps its child without proving that run.
func (a *Adapter) runningChildLocked() bool {
	if a.process == nil {
		return false
	}
	exit := a.process.ExitObservation()
	return a.accountedChildLocked(!a.process.Detached() && !exit.Exited && exit.Err == nil)
}

func (a *Adapter) accountedChildLocked(live bool) bool { return live && a.marker.Epoch == a.statsEpoch }

// lifecycleReadyLocked refuses to open the data plane while a previously recorded
// child core is not proven to have exited.
func (a *Adapter) lifecycleReadyLocked() error {
	if a.detached {
		return core.ErrProcessDetached
	}
	if a.handoverPrepared {
		return core.ErrHandoverConflict
	}
	if a.process != nil && a.process.ExitObservation().Exited {
		return a.finishExitLocked()
	}
	if a.runtimeMaterial != nil && a.runtimeMaterial.State == core.RuntimeExitObserved && a.marker.Run == nil {
		return a.removeRuntimeLocked(a.runtimeMaterial.Run)
	}
	return a.runMarker().Ready(a.runningChildLocked())
}

func (a *Adapter) runIdentityLocked() core.CoreRun {
	if a.marker.Run == nil {
		return core.CoreRun{}
	}
	return *a.marker.Run
}

func (a *Adapter) processErrorLocked() error {
	if a.handoverPrepared {
		return core.ErrHandoverConflict
	}
	if a.detached {
		return core.ErrProcessDetached
	}
	if a.process == nil {
		return nil
	}
	if a.process.Detached() {
		return core.ErrProcessDetached
	}
	exit := a.process.ExitObservation()
	if exit.Exited {
		return errors.Join(errors.New("core exited"), exit.Err)
	}
	return exit.Err
}

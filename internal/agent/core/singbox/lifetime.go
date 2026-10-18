package singbox

import (
	"errors"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/platform/id"
)

// observeCoreRunLocked loads the durable marker if it is not already in memory
// and answers whether this adapter may open the data plane. It never removes the
// marker; cleanup belongs to the owning parent after an observed exit.
func (a *Adapter) observeCoreRunLocked() (core.CoreRunObservation, error) {
	if a.coreRun == nil {
		run, key, err := core.LoadCoreRun(a.Directory)
		if err != nil {
			a.coreRunPending = true
			return core.CoreRunBlocked, errors.Join(core.ErrCoreStopUnconfirmed, err)
		}
		a.coreRun, a.coreRunKey = run, key
	}
	if a.coreRun == nil {
		a.coreRunPending = false
		return core.CoreRunAbsent, nil
	}
	// The marker is bound to one node. A foreign node identity is never this
	// node's exit material and is never removed or overwritten.
	if a.coreRun.NodeID != a.NodeID {
		a.coreRunPending = true
		return core.CoreRunBlocked, core.ErrCoreStopUnconfirmed
	}
	if a.ownedChildMatchesCoreRunLocked() {
		a.coreRunPending = false
		return core.CoreRunAbsent, nil
	}
	// The marker may name a different core after a legitimate core switch. A
	// differing name alone is never a clean exit; only a proven exit of the
	// recorded child (resolved in this observation environment) clears it.
	if core.ResolveCoreRun(a.coreRun, core.ObserveOSEnvironment()) == core.ProcessExited {
		a.coreRunPending = true
		return core.CoreRunExited, nil
	}
	a.coreRunPending = true
	return core.CoreRunBlocked, core.ErrCoreStopUnconfirmed
}

// ownedChildMatchesCoreRunLocked proves the in-memory running child is exactly the
// process recorded in the durable marker. It binds command, PID, OS token,
// environment, epoch and revision; a bare live pointer or a coincidentally equal
// epoch is not enough.
func (a *Adapter) ownedChildMatchesCoreRunLocked() bool {
	run := a.coreRun
	if run == nil || a.process == nil || a.runToken == "" || a.runPID <= 0 || a.runEpoch == "" {
		return false
	}
	return run.NodeID == a.NodeID && run.Core == a.Name() &&
		run.Process.PID == a.runPID && run.Process.Token == a.runToken &&
		run.Environment == a.runEnv &&
		run.Epoch == a.runEpoch && run.Epoch == a.statsEpoch && run.Revision == a.runRevision
}

// lifecycleReadyLocked refuses to open the data plane while a previously
// recorded child core is not proven to have exited. It is independent of the
// user budget and never signals, kills or writes off consumption.
func (a *Adapter) lifecycleReadyLocked() error {
	observation, err := a.observeCoreRunLocked()
	if err != nil {
		return err
	}
	if observation == core.CoreRunExited {
		if err := a.confirmCoreExitLocked(); err != nil {
			return errors.Join(core.ErrCoreStopUnconfirmed, err)
		}
	}
	return nil
}

func (a *Adapter) saveCoreRunLocked(run core.CoreRun) error {
	if a.coreRunKey == nil {
		key, err := core.AccountingKeyForSave(a.Directory)
		if err != nil {
			return err
		}
		a.coreRunKey = key
	}
	return core.SaveCoreRun(a.Directory, a.coreRunKey, run)
}

// claimCoreRunLocked durably records a pending start before the child exists, so
// a crash in the start window cannot leave an orphan without a marker.
func (a *Adapter) claimCoreRunLocked(epoch string, revision int64) error {
	token, err := id.New()
	if err != nil {
		return err
	}
	environment := core.ObserveOSEnvironment()
	run := core.CoreRun{Version: core.CoreRunMarkerVersion, Kind: "core-run-marker-v1", NodeID: a.NodeID, Core: a.Name(), Epoch: epoch, Revision: revision, Environment: environment, Process: core.ProcessIdentity{Token: token}}
	if err := a.saveCoreRunLocked(run); err != nil {
		return err
	}
	a.coreRun = &run
	a.runEpoch = epoch
	a.runRevision = revision
	a.runEnv = environment
	a.runPID = 0
	a.runToken = ""
	return nil
}

func (a *Adapter) bindCoreRunLocked(pid int) error {
	if a.coreRun == nil {
		return errors.New("core run marker absent while binding child")
	}
	identity, err := core.ObserveProcessIdentity(pid)
	if err != nil {
		return err
	}
	next := *a.coreRun
	next.Process = core.ProcessIdentity{PID: pid, Token: identity}
	if err := a.saveCoreRunLocked(next); err != nil {
		return err
	}
	a.coreRun = &next
	a.runPID = pid
	a.runToken = identity
	return nil
}

// confirmCoreExitLocked removes the marker only after the owning parent observed
// the exact child's exit. It writes no settlement and clears no consumption. A
// failed removal or directory sync keeps the in-memory marker and is returned so
// the original request never reports a false success.
func (a *Adapter) confirmCoreExitLocked() error {
	if a.coreRun == nil {
		a.coreRunPending = false
		return nil
	}
	if err := core.RemoveCoreRun(a.Directory); err != nil {
		a.coreRunPending = true
		return err
	}
	a.coreRun = nil
	a.coreRunPending = false
	a.runPID, a.runToken, a.runEpoch, a.runRevision = 0, "", "", 0
	a.runEnv = core.OSEnvironment{}
	return nil
}

// abandonCoreRunLocked drops a pre-start marker after the child never launched.
// A failed removal is returned and retains the in-memory marker.
func (a *Adapter) abandonCoreRunLocked() error {
	if a.coreRun == nil {
		return nil
	}
	if err := core.RemoveCoreRun(a.Directory); err != nil {
		a.coreRunPending = true
		return err
	}
	a.coreRun = nil
	a.coreRunPending = false
	a.runPID, a.runToken, a.runEpoch, a.runRevision = 0, "", "", 0
	a.runEnv = core.OSEnvironment{}
	return nil
}

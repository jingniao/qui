package core

import (
	"errors"

	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
)

// RunMarker is the durable core-run lifetime material of one adapter directory:
// the marker that binds one authenticated child identity to a node, core, epoch
// and revision. Every core adapter (xray, sing-box and gost) holds one and binds
// it to its own identity, so this is the only implementation of the marker rules.
//
// The caller passes `live`, its own answer to "do I hold a running child", so the
// marker never has to reach into an adapter's process state.
type RunMarker struct {
	Directory string
	NodeID    string
	Core      string
	Run       *CoreRun
	Key       *vault.Vault
	// Pending reports an unresolved marker: this adapter may not start a child
	// and may not write off the recorded one.
	Pending  bool
	PID      int
	Token    string
	Epoch    string
	Revision int64
	Env      OSEnvironment
}

// Observe loads the durable marker if it is not already in memory and answers
// whether this adapter may open the data plane. It never removes the marker;
// cleanup belongs to the owning parent after an observed exit.
func (m *RunMarker) Observe(live bool) (CoreRunObservation, error) {
	if m.Run == nil {
		run, key, err := LoadCoreRun(m.Directory)
		if err != nil {
			m.Pending = true
			return CoreRunBlocked, errors.Join(ErrCoreStopUnconfirmed, err)
		}
		m.Run, m.Key = run, key
	}
	if m.Run == nil {
		m.Pending = false
		return CoreRunAbsent, nil
	}
	// The marker is bound to one node. A foreign node identity is never this
	// node's exit material and is never removed or overwritten.
	if m.Run.NodeID != m.NodeID {
		m.Pending = true
		return CoreRunBlocked, ErrCoreStopUnconfirmed
	}
	if m.OwnedChild(live) {
		m.Pending = false
		return CoreRunAbsent, nil
	}
	// The marker may name a different core after a legitimate core switch. A
	// differing name alone is never a clean exit; only a proven exit of the
	// recorded child (resolved in this observation environment) clears it.
	if ResolveCoreRun(m.Run, ObserveOSEnvironment()) == ProcessExited {
		m.Pending = true
		return CoreRunExited, nil
	}
	m.Pending = true
	return CoreRunBlocked, ErrCoreStopUnconfirmed
}

// OwnedChild proves the in-memory running child is exactly the process recorded
// in the durable marker. It binds command, PID, OS token, environment, epoch and
// revision; a bare live pointer or a coincidentally equal epoch is not enough.
func (m *RunMarker) OwnedChild(live bool) bool {
	run := m.Run
	if run == nil || !live || m.Token == "" || m.PID <= 0 || m.Epoch == "" {
		return false
	}
	return run.NodeID == m.NodeID && run.Core == m.Core &&
		run.Process.PID == m.PID && run.Process.Token == m.Token &&
		run.Environment == m.Env && run.Epoch == m.Epoch && run.Revision == m.Revision
}

// Ready refuses to open the data plane while a previously recorded child core is
// not proven to have exited. It is independent of the user budget and never
// signals, kills or writes off consumption.
func (m *RunMarker) Ready(live bool) error {
	observation, err := m.Observe(live)
	if err != nil {
		return err
	}
	if observation == CoreRunExited {
		if err := m.ConfirmExit(); err != nil {
			return errors.Join(ErrCoreStopUnconfirmed, err)
		}
	}
	return nil
}

func (m *RunMarker) save(run CoreRun) error {
	if m.Key == nil {
		key, err := AccountingKeyForSave(m.Directory)
		if err != nil {
			return err
		}
		m.Key = key
	}
	return SaveCoreRun(m.Directory, m.Key, run)
}

// Claim durably records a pending start before the child exists, so a crash in
// the start window cannot leave an orphan without a marker.
func (m *RunMarker) Claim(epoch string, revision int64) error {
	token, err := id.New()
	if err != nil {
		return err
	}
	environment := ObserveOSEnvironment()
	run := CoreRun{Version: CoreRunMarkerVersion, Kind: "core-run-marker-v1", NodeID: m.NodeID, Core: m.Core, Epoch: epoch, Revision: revision, Environment: environment, Process: ProcessIdentity{Token: token}}
	if err := m.save(run); err != nil {
		return err
	}
	m.Run = &run
	m.Epoch, m.Revision, m.Env = epoch, revision, environment
	m.PID, m.Token = 0, ""
	return nil
}

// Bind records the exact launched child's OS identity. A failure keeps a marker
// a later parent must treat as unknown rather than a clean exit.
func (m *RunMarker) Bind(pid int) error {
	if m.Run == nil {
		return errors.New("core run marker absent while binding child")
	}
	identity, err := ObserveProcessIdentity(pid)
	if err != nil {
		return err
	}
	next := *m.Run
	next.Process = ProcessIdentity{PID: pid, Token: identity}
	if err := m.save(next); err != nil {
		return err
	}
	m.Run = &next
	m.PID, m.Token = pid, identity
	return nil
}

// ConfirmExit removes the marker only after the owning parent observed the exact
// child's exit. It writes no settlement and clears no consumption. A failed
// removal or directory sync keeps the in-memory marker and is returned so the
// original request never reports a false success.
func (m *RunMarker) ConfirmExit() error {
	m.Pending = false
	if m.Run == nil {
		return nil
	}
	if err := RemoveCoreRun(m.Directory); err != nil {
		m.Pending = true
		return err
	}
	m.Clear()
	return nil
}

// Abandon drops a pre-start marker after the child never launched. A failed
// removal is returned and retains the in-memory marker.
func (m *RunMarker) Abandon() error {
	if m.Run == nil {
		return nil
	}
	if err := RemoveCoreRun(m.Directory); err != nil {
		m.Pending = true
		return err
	}
	m.Pending = false
	m.Clear()
	return nil
}

func (m *RunMarker) Clear() {
	m.Run = nil
	m.Pending = false
	m.PID, m.Token, m.Epoch, m.Revision = 0, "", "", 0
	m.Env = OSEnvironment{}
}

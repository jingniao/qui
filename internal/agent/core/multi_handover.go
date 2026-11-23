package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"proxycontrol/pkg/contracts/desiredstate"
)

type coordinatedHandover interface {
	HandoverRuntime
	RestoreHandoverInputs(RuntimeMaterial) error
	HandoverMatches(RuntimeMaterial, RenderedConfig) (bool, error)
	AdoptForRestart(context.Context, RuntimeMaterial, CoreRun) error
	ResumePreparedHandover()
	ReleaseHandoverOwnership() error
	CheckPreparedHandover() error
	ReclaimDetachedForStop(context.Context) error
}

// AddStartupRuntime supplies an old disabled core's management handle without
// enabling it in inventory or making it available to the candidate renderer.
func (m *MultiRuntime) AddStartupRuntime(r Runtime) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initialized || r == nil || !KnownName(r.Name()) || m.runtimes[r.Name()] != nil {
		return ErrHandoverConflict
	}
	if m.startupOnly == nil {
		m.startupOnly = map[string]bool{}
	}
	if allocator, ok := r.(interface{ SetPortAllocator(*PortAllocator) }); ok {
		allocator.SetPortAllocator(m.ports)
	}
	r.ConfigureRuntime(m.hooks)
	m.runtimes[r.Name()], m.startupOnly[r.Name()] = r, true
	m.names = append(m.names, r.Name())
	return nil
}

// FinishStartupRuntimes follows successful publication, which has already
// sampled and stopped every old core absent from the enabled inventory.
func (m *MultiRuntime) FinishStartupRuntimes() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name := range m.startupOnly {
		if m.expected[name] {
			return ErrHandoverConflict
		}
	}
	names := m.names[:0]
	for _, name := range m.names {
		if m.startupOnly[name] {
			delete(m.runtimes, name)
			delete(m.current, name)
			delete(m.expected, name)
		} else {
			names = append(names, name)
		}
	}
	m.names, m.startupOnly = names, nil
	return nil
}

// HandoverRenderedEqual includes account ownership and collector metadata, not
// just native JSON. JSON compares the persisted metadata without depending on
// pointer identity or map iteration order.
func HandoverRenderedEqual(a, b RenderedConfig) bool {
	x, e1 := json.Marshal(a)
	y, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && string(x) == string(y)
}

func HandoverFailureCode(err error) string {
	for _, item := range []struct {
		err  error
		code string
	}{
		{ErrHandoverUnsupported, CodeHandoverUnsupported}, {ErrHandoverPermission, CodeHandoverPermission},
		{ErrHandoverExpired, CodeHandoverExpired}, {ErrHandoverConflict, CodeHandoverConflict},
		{ErrCoreExitedDuringHandover, CodeCoreExitedDuringHandover},
	} {
		if errors.Is(err, item.err) {
			return item.code
		}
	}
	return CodeHandoverInvalid
}

func (m *MultiRuntime) HandoverInProgress() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handoverBusy || m.handoverErr != nil
}

func (m *MultiRuntime) PrepareHandover(ctx context.Context) ([]HandoverCoreReceipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handoverBusy || m.handoverErr != nil {
		return nil, errors.Join(ErrHandoverConflict, m.handoverErr)
	}
	m.handoverBusy = true
	receipts := []HandoverCoreReceipt{}
	m.handoverPrepared = nil
	for _, name := range m.names {
		if !m.expected[name] {
			continue
		}
		r, ok := m.runtimes[name].(coordinatedHandover)
		var receipt HandoverCoreReceipt
		var err error
		if !ok {
			err = ErrHandoverUnsupported
		} else {
			receipt, err = r.PrepareHandover(ctx)
		}
		if err != nil {
			for _, prepared := range m.handoverPrepared {
				m.runtimes[prepared].(coordinatedHandover).ResumePreparedHandover()
			}
			m.handoverPrepared, m.handoverBusy = nil, false
			return nil, fmt.Errorf("%s prepare: %w", name, err)
		}
		m.handoverPrepared = append(m.handoverPrepared, name)
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

func (m *MultiRuntime) DetachForHandover() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.handoverBusy {
		return ErrHandoverConflict
	}
	for _, name := range m.handoverPrepared {
		if err := m.runtimes[name].(coordinatedHandover).CheckPreparedHandover(); err != nil {
			return fmt.Errorf("%s detach preflight: %w", name, err)
		}
	}
	for _, name := range m.handoverPrepared {
		if err := m.runtimes[name].(coordinatedHandover).DetachForHandover(); err != nil {
			return fmt.Errorf("%s detach: %w", name, err)
		}
	}
	return nil
}

// CancelHandover restores ownership before ordinary Close on a failed commit.
func (m *MultiRuntime) CancelHandover(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var err error
	for _, name := range m.handoverPrepared {
		r := m.runtimes[name].(coordinatedHandover)
		err = errors.Join(err, r.ReclaimDetachedForStop(ctx))
		r.ResumePreparedHandover()
	}
	m.handoverBusy, m.handoverPrepared = false, nil
	return err
}

// RestoreStartup is a separate transaction from Apply: failures release only
// observers, never restart successfully checked peers as compensation.
func (m *MultiRuntime) RestoreStartup(ctx context.Context, state desiredstate.State, materials map[string]RuntimeMaterial) (candidate RenderedConfig, changed bool, resultErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handoverBusy || m.handoverErr != nil || len(m.current) != 0 {
		return candidate, false, ErrHandoverConflict
	}
	m.handoverBusy = true
	adopted := []string{}
	defer func() {
		m.handoverBusy = false
		if resultErr != nil {
			for _, name := range adopted {
				resultErr = errors.Join(resultErr, m.runtimes[name].(coordinatedHandover).ReleaseHandoverOwnership())
			}
			m.handoverErr = resultErr
		}
	}()
	// Each saved guard set is the whole node set. Only ownership purposes are
	// unique; repeated guard-N entries must agree with an owning core.
	owners := map[int]string{}
	guards := map[int]bool{}
	for name, material := range materials {
		if _, ok := m.runtimes[name].(coordinatedHandover); !ok {
			return candidate, false, fmt.Errorf("%s: %w", name, ErrHandoverUnsupported)
		}
		if err := material.Validate(); err != nil {
			return candidate, false, fmt.Errorf("%s material: %w", name, err)
		}
		if material.RenderMetadataVersion != 1 {
			return candidate, false, ErrHandoverInvalid
		}
		if material.State != RuntimeActive || material.Run.Core != name || material.Run.NodeID != state.NodeID || material.Run.Revision != state.Revision {
			return candidate, false, ErrHandoverConflict
		}
		if ResolveCoreRun(&material.Run, ObserveOSEnvironment()) != ProcessAlive {
			return candidate, false, errors.Join(ErrCoreStopUnconfirmed, ErrCoreExitedDuringHandover)
		}
		for _, port := range material.PrivatePorts {
			guards[port.Port] = true
			if port.Purpose == fmt.Sprintf("guard-%d", port.Port) {
				continue
			}
			if owners[port.Port] != "" {
				return candidate, false, ErrHandoverConflict
			}
			owners[port.Port] = name
		}
	}
	ports := []int{}
	for port := range guards {
		// The allocator also retains guard-only reservations after a core was
		// removed. Preserve them without inventing a live listener owner.
		ports = append(ports, port)
	}
	for _, material := range materials {
		if len(material.PrivatePorts) != len(guards) {
			return candidate, false, ErrHandoverConflict
		}
	}
	if err := m.ports.AdoptPrivatePorts(ports); err != nil {
		return candidate, false, err
	}
	for _, name := range m.names {
		if material, ok := materials[name]; ok {
			if err := m.runtimes[name].(coordinatedHandover).RestoreHandoverInputs(material); err != nil {
				return candidate, false, fmt.Errorf("%s inputs: %w", name, err)
			}
		}
	}
	candidate, resultErr = m.render(ctx, state)
	for _, name := range m.names {
		if r, ok := m.runtimes[name].(interface{ EndHandoverRender() }); ok {
			r.EndHandoverRender()
		}
	}
	if resultErr != nil {
		return candidate, false, resultErr
	}
	if err := m.validate(ctx, candidate); err != nil {
		return candidate, false, err
	}
	changed = len(candidate.Cores) != len(materials)
	for _, name := range m.names {
		material, old := materials[name]
		next, active := candidate.Cores[name]
		if old != active {
			changed = true
			slog.Info("handover configuration changed", "event", CodeHandoverConfigChanged, "core", name, "category", "membership")
			continue
		}
		if !old {
			continue
		}
		matches, err := m.runtimes[name].(coordinatedHandover).HandoverMatches(material, next)
		if err != nil {
			return candidate, false, fmt.Errorf("%s compare: %w", name, err)
		}
		changed = changed || !matches
		if !matches {
			category := "installed_inputs"
			if !HandoverRenderedEqual(material.Rendered, next) {
				category = "rendered_configuration_or_ownership"
			}
			slog.Info("handover configuration changed", "event", CodeHandoverConfigChanged, "core", name, "category", category)
		}
	}
	if changed {
		slog.Info("handover configuration changed; publishing all cores", "event", CodeHandoverConfigChanged, "revision", state.Revision)
	}
	for _, name := range m.names {
		material, ok := materials[name]
		if !ok {
			continue
		}
		r := m.runtimes[name].(coordinatedHandover)
		var err error
		if changed {
			err = r.AdoptForRestart(ctx, material, material.Run)
		} else {
			err = r.Adopt(ctx, material, material.Run)
		}
		if err != nil {
			return candidate, changed, fmt.Errorf("%s adopt: %w", name, err)
		}
		adopted = append(adopted, name)
	}
	for _, name := range adopted {
		health, err := m.runtimes[name].Health(ctx)
		if err != nil || !health.Healthy {
			if err == nil {
				err = errors.New("adopted core is unhealthy")
			}
			return candidate, changed, fmt.Errorf("%s adoption commit: %w", name, err)
		}
	}
	// Commit membership only after every old run has sampled into the spool.
	m.current = map[string]RenderedConfig{}
	old := RenderedConfig{NodeID: state.NodeID, Revision: state.Revision, Cores: map[string]RenderedConfig{}}
	for _, name := range m.names {
		material, ok := materials[name]
		m.expected[name] = ok
		if ok {
			m.current[name], old.Cores[name] = material.Rendered, material.Rendered
		}
	}
	m.initialized = true
	if !changed {
		m.history[state.Revision] = candidate
	} else {
		m.history[state.Revision] = old
	}
	return candidate, changed, nil
}

// BlockHandover retains the original failure on the status surface and refuses
// later publication/supervision. Close still keeps ordinary stop semantics.
func (m *MultiRuntime) BlockHandover(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handoverErr = err
}

func (m *MultiRuntime) ReleaseHandoverOwnership() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var err error
	for _, name := range m.names {
		if r, ok := m.runtimes[name].(coordinatedHandover); ok {
			err = errors.Join(err, r.ReleaseHandoverOwnership())
		}
	}
	return err
}

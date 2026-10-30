package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
)

// MultiRuntime is one revision transaction over independent, fixed adapters.
// Ordering is config ordering; the first adapter is the compatibility primary.
type MultiRuntime struct {
	hooks       RuntimeHooks
	history     map[int64]RenderedConfig
	mu          sync.Mutex
	primary     string
	names       []string
	runtimes    map[string]Runtime
	current     map[string]RenderedConfig
	expected    map[string]bool
	ports       *PortAllocator
	initialized bool
}

func NewMultiRuntime(runtimes ...Runtime) (*MultiRuntime, error) {
	m := &MultiRuntime{runtimes: map[string]Runtime{}, current: map[string]RenderedConfig{}, history: map[int64]RenderedConfig{}, expected: map[string]bool{}, ports: &PortAllocator{}}
	for _, r := range runtimes {
		if r == nil || (r.Name() != "xray" && r.Name() != "sing-box") || m.runtimes[r.Name()] != nil {
			return nil, errors.New("invalid multi-core adapter set")
		}
		if m.primary == "" {
			m.primary = r.Name()
		}
		m.names = append(m.names, r.Name())
		m.runtimes[r.Name()] = r
		if a, ok := r.(interface{ SetPortAllocator(*PortAllocator) }); ok {
			a.SetPortAllocator(m.ports)
		}
	}
	if m.primary == "" {
		return nil, errors.New("empty multi-core adapter set")
	}
	return m, nil
}
func (m *MultiRuntime) Name() string { m.mu.Lock(); defer m.mu.Unlock(); return m.primary }
func (m *MultiRuntime) Component(name string) Runtime {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == "" {
		name = m.primary
	}
	return m.runtimes[name]
}
func (m *MultiRuntime) ConfigureRuntime(h RuntimeHooks) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks = h
	for _, name := range m.names {
		m.runtimes[name].ConfigureRuntime(h)
	}
}
func (m *MultiRuntime) DetectCapabilities(ctx context.Context) (*pb.Capabilities, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result *pb.Capabilities
	var cores []*pb.CoreCapability
	for _, name := range m.names {
		c, err := m.runtimes[name].DetectCapabilities(ctx)
		if err != nil || c == nil {
			c = &pb.Capabilities{CoreName: name}
		}
		if name == m.primary {
			result = proto.Clone(c).(*pb.Capabilities)
		}
		cores = append(cores, &pb.CoreCapability{Name: name, Version: c.CoreVersion, Protocols: c.Protocols, SingboxUserStats: c.SingboxUserStats, ForwardInboundSupported: c.ForwardInboundSupported, DeviceGateSupported: c.DeviceGateSupported, ObservationContractVersion: c.ObservationContractVersion})
	}
	result.Cores, result.MultiCoreSupported = cores, true
	return result, nil
}

func (m *MultiRuntime) Render(ctx context.Context, s desiredstate.State) (RenderedConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.render(ctx, s)
}
func (m *MultiRuntime) render(ctx context.Context, s desiredstate.State) (RenderedConfig, error) {
	if err := s.Validate(); err != nil {
		return RenderedConfig{}, err
	}
	ports := []int{}
	initialize := !m.initialized
	if initialize {
		for _, name := range m.names {
			m.expected[name] = false
		}
		m.initialized = true
	}
	for _, in := range s.Inbounds {
		name := in.Core
		if name == "" {
			name = m.primary
		}
		if m.runtimes[name] == nil {
			return RenderedConfig{}, fmt.Errorf("inbound assigns unavailable core %s", name)
		}
		if in.Enabled {
			if initialize {
				m.expected[name] = true
			}
			ports = append(ports, in.Port)
		}
	}
	if err := m.ports.ReserveInbounds(ports); err != nil {
		return RenderedConfig{}, err
	}
	r := RenderedConfig{NodeID: s.NodeID, Revision: s.Revision, Cores: map[string]RenderedConfig{}}
	for _, name := range m.names {
		part := s.ForCore(name, m.primary)
		active := false
		for _, in := range part.Inbounds {
			active = active || in.Enabled
		}

		if !active {
			continue
		}
		c, err := m.runtimes[name].Render(ctx, part)
		if err != nil {
			return r, fmt.Errorf("%s render: %w", name, err)
		}
		for _, in := range part.Inbounds {
			if in.Enabled {
				c.DeviceCredentialIDs = append(c.DeviceCredentialIDs, in.CredentialIDs...)
			}
		}
		r.Cores[name] = c
	}
	// A second render lets every core guard every reserved private listener,
	// including listeners allocated while rendering a later core.
	for _, name := range m.names {
		if previous, ok := r.Cores[name]; ok {
			c, err := m.runtimes[name].Render(ctx, s.ForCore(name, m.primary))
			if err != nil {
				return r, err
			}
			c.DeviceCredentialIDs = previous.DeviceCredentialIDs
			c.PrivatePorts = m.ports.Ports()
			r.Cores[name] = c
		}
	}
	data := map[string]json.RawMessage{}
	for name, c := range r.Cores {
		data[name] = c.Data
	}
	var err error
	r.Data, err = json.Marshal(data)
	return r, err
}

// DecodeCoreConfigs reads both the additive core map and legacy native JSON.
// Rendered metadata is reconstructed from the authenticated desired snapshot.
func DecodeCoreConfigs(data []byte, primary string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, errors.New("invalid core revision")
	}
	multi := true
	for name := range object {
		if name != "xray" && name != "sing-box" {
			multi = false
		}
	}
	if multi {
		return object, nil
	}
	return map[string]json.RawMessage{primary: append([]byte(nil), data...)}, nil
}

func (m *MultiRuntime) Validate(ctx context.Context, c RenderedConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.validate(ctx, c)
}
func (m *MultiRuntime) validate(ctx context.Context, c RenderedConfig) error {
	if c.Cores == nil {
		return errors.New("multi-core rendered metadata missing")
	}
	for name := range c.Cores {
		if m.runtimes[name] == nil {
			return errors.New("unknown rendered core")
		}
	}
	for _, name := range m.names {
		if part, ok := c.Cores[name]; ok {
			if err := m.runtimes[name].Validate(ctx, part); err != nil {
				return fmt.Errorf("%s validation: %w", name, err)
			}
		}
	}
	return nil
}
func (m *MultiRuntime) Apply(ctx context.Context, c RenderedConfig) (ApplyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := ApplyResult{Revision: c.Revision}
	if err := m.validate(ctx, c); err != nil {
		r.Code = "validation_failed"
		return r, err
	}
	switched := []string{}
	for _, name := range m.names {
		next, active := c.Cores[name]
		var err error
		if active {
			_, err = m.runtimes[name].Apply(ctx, next)
		} else {
			err = m.runtimes[name].Close()
		}
		// Include the failing adapter: adapters retain their previous config on
		// ordinary failures, but rollback failure must also be repaired here.
		switched = append(switched, name)
		if err != nil {
			r.Code = "apply_failed"
			undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			var undo error
			for i := len(switched) - 1; i >= 0; i-- {
				n := switched[i]
				if previous, ok := m.current[n]; ok {
					_, e := m.runtimes[n].Apply(undoCtx, previous)
					undo = errors.Join(undo, e)
				} else {
					undo = errors.Join(undo, m.runtimes[n].Close())
				}
			}
			r.RolledBack = undo == nil
			if undo != nil {
				r.Code = "rollback_failed"
			}
			return r, errors.Join(err, undo)
		}
	}
	m.initialized = true
	m.current = map[string]RenderedConfig{}
	for _, name := range m.names {
		part, ok := c.Cores[name]
		m.expected[name] = ok
		if ok {
			m.current[name] = part
		}
	}
	m.history[c.Revision] = c
	r.Success, r.Code = true, "success"
	return r, nil
}
func (m *MultiRuntime) Rollback(ctx context.Context, revision int64) error {
	m.mu.Lock()
	c, ok := m.history[revision]
	m.mu.Unlock()
	if !ok {
		return errors.New("core revision not retained")
	}
	_, err := m.Apply(ctx, c)
	return err
}
func (m *MultiRuntime) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var err error
	for _, name := range m.names {
		err = errors.Join(err, m.runtimes[name].Close())
	}
	return err
}
func (m *MultiRuntime) CoreStatuses(ctx context.Context) []*pb.CoreStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := []*pb.CoreStatus{}
	for _, name := range m.names {
		r := m.runtimes[name]
		status := &pb.CoreStatus{Name: name, Healthy: true}
		if m.expected[name] {
			h, err := r.Health(ctx)
			status.Healthy = err == nil && h.Healthy
			if !status.Healthy {
				status.Error = "core_unhealthy"
				if errors.Is(err, ErrCoreStopUnconfirmed) {
					status.Error = "core_lifecycle_unconfirmed"
				}
			}
		}
		if source, ok := r.(interface{ CoreStartedAt() time.Time }); ok {
			if at := source.CoreStartedAt(); !at.IsZero() {
				status.StartedAt = timestamppb.New(at)
			}
		}
		if source, ok := r.(interface {
			TrafficSourceStatus() *pb.TrafficSourceStatus
		}); ok {
			status.TrafficSourceStatus = source.TrafficSourceStatus()
		}
		items = append(items, status)
	}
	return items
}
func (m *MultiRuntime) Health(ctx context.Context) (HealthStatus, error) {
	h := HealthStatus{Healthy: true, CheckedAt: time.Now()}
	var err error
	for _, s := range m.CoreStatuses(ctx) {
		h.Healthy = h.Healthy && s.Healthy
		if s.Error == "core_lifecycle_unconfirmed" {
			err = ErrCoreStopUnconfirmed
		}
	}
	return h, err
}
func (m *MultiRuntime) RestoreCore(ctx context.Context, name string, state desiredstate.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runtimes[name] == nil {
		return errors.New("unknown core")
	}
	// Reserve node-wide listeners, then render and switch only the selected core.
	ports := []int{}
	for _, in := range state.Inbounds {
		if in.Enabled {
			ports = append(ports, in.Port)
		}
	}
	if err := m.ports.ReserveInbounds(ports); err != nil {
		return err
	}
	subset := state.ForCore(name, m.primary)
	active := false
	for _, in := range subset.Inbounds {
		active = active || in.Enabled
	}
	var part RenderedConfig
	var err error
	if active {
		part, err = m.runtimes[name].Render(ctx, subset)
		if err != nil {
			return err
		}
		part.PrivatePorts = m.ports.Ports()
		// A targeted binary update can discover a new API/probe listener. Do
		// not open it while a healthy peer still has the old guard set. An
		// ordinary all-core publication can install the expanded guards first.
		for peer, previous := range m.current {
			if peer == name {
				continue
			}
			guarded := map[int]bool{}
			for _, port := range previous.PrivatePorts {
				guarded[port] = true
			}
			for _, port := range part.PrivatePorts {
				if !guarded[port] {
					return errors.New("new private listener requires an all-core revision before targeted activation")
				}
			}
		}
	}
	if !active {
		if err := m.runtimes[name].Close(); err != nil {
			return err
		}
		delete(m.current, name)
		m.expected[name] = false
		return nil
	}
	if err := m.runtimes[name].Validate(ctx, part); err != nil {
		return err
	}
	if _, err := m.runtimes[name].Apply(ctx, part); err != nil {
		return err
	}
	for _, in := range subset.Inbounds {
		if in.Enabled {
			part.DeviceCredentialIDs = append(part.DeviceCredentialIDs, in.CredentialIDs...)
		}
	}
	m.current[name], m.expected[name] = part, true
	return nil
}
func (m *MultiRuntime) SupportsAccountTelemetry() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, name := range m.names {
		if m.expected[name] {
			return true
		}
	}
	return false
}
func (m *MultiRuntime) SampleTraffic(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var err error
	for _, name := range m.names {
		if m.expected[name] {
			err = errors.Join(err, m.runtimes[name].SampleTraffic(ctx))
		}
	}
	return err
}
func (m *MultiRuntime) CollectStats(ctx context.Context) (StatsBatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total StatsBatch
	var err error
	for _, name := range m.names {
		if !m.expected[name] {
			continue
		}
		s, e := m.runtimes[name].CollectStats(ctx)
		err = errors.Join(err, e)
		total.Upload += s.Upload
		total.Download += s.Download
		total.Connections += s.Connections
	}
	return total, err
}
func (m *MultiRuntime) OutboundHealth() map[string]scheduler.Health {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]scheduler.Health{}
	for _, name := range m.names {
		for id, h := range m.runtimes[name].OutboundHealth() {
			if old, ok := result[id]; ok {
				h.Healthy = h.Healthy && old.Healthy
			}
			result[id] = h
		}
	}
	return result
}

var _ Runtime = (*MultiRuntime)(nil)

func (m *MultiRuntime) CoreActive(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.expected[name]
}

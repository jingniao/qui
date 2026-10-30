package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/devicegate"
	"proxycontrol/pkg/contracts/traffic"
)

func (m *MultiRuntime) UnconfirmedStop() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, name := range m.names {
		if r, ok := m.runtimes[name].(interface{ UnconfirmedStop() bool }); ok && r.UnconfirmedStop() {
			return true
		}
	}
	return false
}
func (m *MultiRuntime) CoreStartedAt() time.Time {
	if r, ok := m.Component("").(interface{ CoreStartedAt() time.Time }); ok {
		return r.CoreStartedAt()
	}
	return time.Time{}
}
func (m *MultiRuntime) TrafficSourceStatus() *pb.TrafficSourceStatus {
	if r, ok := m.Component("").(interface {
		TrafficSourceStatus() *pb.TrafficSourceStatus
	}); ok {
		return r.TrafficSourceStatus()
	}
	return nil
}
func (m *MultiRuntime) Probe(ctx context.Context) error { return m.ProbeExcept(ctx, nil) }
func (m *MultiRuntime) ProbeExcept(ctx context.Context, held map[string]bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var err error
	for _, name := range m.names {
		if m.expected[name] && !held[name] {
			if r, ok := m.runtimes[name].(interface{ Probe(context.Context) error }); ok {
				err = errors.Join(err, r.Probe(ctx))
			}
		}
	}
	return err
}
func (m *MultiRuntime) SampleConnections(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, name := range m.names {
		if m.expected[name] {
			if r, ok := m.runtimes[name].(interface{ SampleConnections(context.Context) }); ok {
				r.SampleConnections(ctx)
			}
		}
	}
}

func (m *MultiRuntime) ObservedConnections(ctx context.Context) (traffic.ConnectionSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	parts := []traffic.ConnectionSnapshot{}
	for _, name := range m.names {
		if !m.expected[name] {
			continue
		}
		r, ok := m.runtimes[name].(interface {
			ObservedConnections(context.Context) (traffic.ConnectionSnapshot, error)
		})
		if !ok {
			return traffic.ConnectionSnapshot{}, errors.New("core connection telemetry unavailable")
		}
		s, err := r.ObservedConnections(ctx)
		if err != nil {
			return traffic.ConnectionSnapshot{}, err
		}
		parts = append(parts, s)
	}
	return MergeConnections(parts)
}

// CoreConnections keeps measured peers available when another core is down.
// Missing entries mean unknown; no zero snapshot is fabricated for a failure.
func (m *MultiRuntime) CoreConnections(ctx context.Context) map[string]traffic.ConnectionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]traffic.ConnectionSnapshot{}
	for _, name := range m.names {
		if !m.expected[name] {
			continue
		}
		if r, ok := m.runtimes[name].(interface {
			ObservedConnections(context.Context) (traffic.ConnectionSnapshot, error)
		}); ok {
			if s, err := r.ObservedConnections(ctx); err == nil && s.Validate() == nil {
				result[name] = s
			}
		}
	}
	return result
}

// MergeConnections deduplicates address keys while adding session counts. A
// missing measurement remains unknown; it must never become a measured zero.
func MergeConnections(parts []traffic.ConnectionSnapshot) (traffic.ConnectionSnapshot, error) {
	if len(parts) == 0 {
		return traffic.ConnectionSnapshot{}, errors.New("no active core snapshots")
	}
	r := parts[0]
	r.Accounts = nil
	r.Connections = 0
	epochs := []string{}
	accounts := map[string]traffic.ConnectionAccount{}
	for _, p := range parts {
		if err := p.Validate(); err != nil {
			return r, err
		}
		if p.NodeID != r.NodeID || p.Revision != r.Revision {
			return r, errors.New("core snapshot identity mismatch")
		}
		r.DeviceGateSupported = r.DeviceGateSupported && p.DeviceGateSupported
		if p.ObservationContractVersion < r.ObservationContractVersion {
			r.ObservationContractVersion = p.ObservationContractVersion
		}
		if p.ObservedAt.Before(r.ObservedAt) {
			r.ObservedAt = p.ObservedAt
		}
		epochs = append(epochs, p.CoreEpoch)
		for _, a := range p.Accounts {
			old, exists := accounts[a.CredentialID]
			if !exists {
				old = a
				old.AddressKeys = append([]string(nil), a.AddressKeys...)
			} else {
				old.Connections += a.Connections
				old.AddressesMeasured = old.AddressesMeasured && a.AddressesMeasured
				keys := map[string]bool{}
				for _, k := range old.AddressKeys {
					keys[k] = true
				}
				for _, k := range a.AddressKeys {
					if !keys[k] {
						old.AddressKeys = append(old.AddressKeys, k)
						keys[k] = true
					}
				}
			}
			accounts[a.CredentialID] = old
		}
	}
	sort.Strings(epochs)
	data, _ := json.Marshal(epochs)
	sum := sha256.Sum256(data)
	r.CoreEpoch = hex.EncodeToString(sum[:])
	ids := []string{}
	for id := range accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a := accounts[id]
		if !a.AddressesMeasured || r.ObservationContractVersion != 2 {
			a.AddressKeys = nil
			a.AddressesMeasured = false
		}
		sort.Strings(a.AddressKeys)
		r.Accounts = append(r.Accounts, a)
		r.Connections += a.Connections
	}
	if r.ObservationContractVersion != 2 {
		r.DeviceGateSupported = false
	}
	return r, r.Validate()
}
func (m *MultiRuntime) SetDeviceGate(ctx context.Context, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	request, decodeErr := devicegate.Decode(data)
	var err error
	for _, name := range m.names {
		if !m.expected[name] {
			continue
		}
		target, ok := m.runtimes[name].(interface {
			SetDeviceGate(context.Context, []byte) error
		})
		if !ok {
			continue
		}
		part := devicegate.Request{TTLSeconds: request.TTLSeconds, Entries: []devicegate.Entry{}}
		for _, e := range request.Entries {
			ids := m.current[name].DeviceCredentialIDs
			if ids == nil {
				for _, a := range m.current[name].StatsAccounts {
					ids = append(ids, a.CredentialID)
				}
			}
			for _, id := range ids {
				if id == e.CredentialID {
					part.Entries = append(part.Entries, e)
					break
				}
			}
		}
		payload, _ := json.Marshal(part)
		if decodeErr != nil {
			payload = data
		} // invalid input revokes each lease
		err = errors.Join(err, target.SetDeviceGate(ctx, payload))
	}
	return errors.Join(decodeErr, err)
}
func (m *MultiRuntime) TickDeviceGates(ctx context.Context) error {
	return m.TickDeviceGatesExcept(ctx, nil)
}
func (m *MultiRuntime) TickDeviceGatesExcept(ctx context.Context, held map[string]bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var err error
	for _, name := range m.names {
		if m.expected[name] && !held[name] {
			if r, ok := m.runtimes[name].(interface{ TickDeviceGates(context.Context) error }); ok {
				err = errors.Join(err, r.TickDeviceGates(ctx))
			}
		}
	}
	return err
}

// NodeTrafficTotals preserves the legacy single sing-box heartbeat surface. A
// mixed node uses negotiated per-core spool batches: a partial aggregate must
// never masquerade as the whole node's measured total.
func (m *MultiRuntime) NodeTrafficTotals(ctx context.Context) (uint64, uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.names) != 1 {
		return 0, 0, errors.New("mixed-node totals require per-core accounting")
	}
	if source, ok := m.runtimes[m.primary].(interface {
		NodeTrafficTotals(context.Context) (uint64, uint64, error)
	}); ok {
		return source.NodeTrafficTotals(ctx)
	}
	return 0, 0, errors.New("node traffic totals unavailable")
}

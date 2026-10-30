package connections

import (
	"errors"
	"sort"
	"strings"

	"proxycontrol/pkg/contracts/traffic"
)

// RecordCores replaces a complete volatile measurement, retaining measured peers
// even when another core cannot be observed. Never persist raw address keys.
func (s *Store) RecordCores(nodeID string, cores map[string]traffic.ConnectionSnapshot) error {
	if len(cores) == 0 || len(cores) > 2 {
		return errors.New("invalid core snapshots")
	}
	names := []string{}
	for name, snapshot := range cores {
		if name != "xray" && name != "sing-box" || snapshot.NodeID != nodeID {
			return ErrNodeMismatch
		}
		if err := snapshot.Validate(); err != nil {
			return err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	merged := cores[names[0]]
	merged.Accounts = nil
	merged.Connections = 0
	merged.CoreEpoch = ""
	merged.DeviceGateSupported = false
	accounts := map[string]traffic.ConnectionAccount{}
	epochs := []string{}
	for _, name := range names {
		snapshot := cores[name]
		if snapshot.Revision != merged.Revision {
			return errors.New("mixed snapshot revisions")
		}
		epochs = append(epochs, snapshot.CoreEpoch)
		merged.Connections += snapshot.Connections
		if snapshot.DeviceGateSupported {
			merged.DeviceGateSupported = true
			merged.ObservationContractVersion = 2
		}
		if snapshot.ObservedAt.Before(merged.ObservedAt) {
			merged.ObservedAt = snapshot.ObservedAt
		}
		for _, a := range snapshot.Accounts {
			old := accounts[a.CredentialID]
			old.CredentialID = a.CredentialID
			old.Connections += a.Connections
			old.AddressesMeasured = old.AddressesMeasured || a.AddressesMeasured
			keys := map[string]bool{}
			for _, key := range old.AddressKeys {
				keys[key] = true
			}
			for _, key := range a.AddressKeys {
				keys[key] = true
			}
			old.AddressKeys = nil
			for key := range keys {
				old.AddressKeys = append(old.AddressKeys, key)
			}
			sort.Strings(old.AddressKeys)
			accounts[a.CredentialID] = old
		}
	}
	merged.CoreEpoch = strings.Join(epochs, ".")
	if len(merged.CoreEpoch) > 128 {
		merged.CoreEpoch = merged.CoreEpoch[:128]
	}
	ids := []string{}
	for id := range accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		merged.Accounts = append(merged.Accounts, accounts[id])
	}
	if err := merged.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cores == nil {
		s.cores = map[string]map[string]entry{}
	}
	s.cores[nodeID] = map[string]entry{}
	for name, snapshot := range cores {
		s.cores[nodeID][name] = entry{snapshot: snapshot, received: s.now()}
	}
	if s.nodes == nil {
		s.nodes = map[string]entry{}
	}
	s.nodes[nodeID] = entry{snapshot: merged, received: s.now()}
	return nil
}
func (s *Store) CoreSnapshots(nodeID string) map[string]traffic.ConnectionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := map[string]traffic.ConnectionSnapshot{}
	for name, e := range s.cores[nodeID] {
		if !s.stale(e.received) {
			result[name] = e.snapshot
		}
	}
	return result
}

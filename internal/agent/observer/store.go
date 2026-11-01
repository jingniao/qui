// Package observer holds volatile core observations between sub-second core polls
// and fifteen-second control heartbeats. Nothing in this package writes to disk.
package observer

import (
	"errors"
	"proxycontrol/pkg/contracts/traffic"
	"sort"
	"sync"
	"time"
)

const (
	// AddressRetentionWindow is how long a source stays in the snapshot after
	// the last sample that saw it: the current 15 s heartbeat plus margin.
	AddressRetentionWindow = 20 * time.Second
	// snapshotFreshness is how long a recorded sample is served at all.
	snapshotFreshness = 10 * time.Second
	// maxAddressKeys is the per-account key limit of
	// traffic.ConnectionSnapshot.Validate, so a retained union stays valid.
	maxAddressKeys = 4096
)

type Store struct {
	mu       sync.Mutex
	snapshot traffic.ConnectionSnapshot
	// retained holds, per credential, the address keys recent samples saw and
	// when each was last seen. Record rebuilds it from the latest sample, so a
	// credential that sample no longer reports loses its keys.
	retained map[string]map[string]time.Time
	received time.Time
	Now      func() time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func copySnapshot(v traffic.ConnectionSnapshot) traffic.ConnectionSnapshot {
	v.Accounts = append([]traffic.ConnectionAccount(nil), v.Accounts...)
	for i := range v.Accounts {
		v.Accounts[i].AddressKeys = append([]string(nil), v.Accounts[i].AddressKeys...)
	}
	return v
}

// Record replaces the snapshot with the latest sample. Connection counts come
// from that sample alone, while the address keys of the retention window are
// merged in: a source a single sample caught must not disappear at the next one.
func (s *Store) Record(v traffic.ConnectionSnapshot, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if err != nil || v.Validate() != nil {
		s.snapshot = traffic.ConnectionSnapshot{}
		s.retained = nil
		s.received = time.Time{}
		return
	}
	// Another process or revision observes a different set of live sources.
	if v.NodeID != s.snapshot.NodeID || v.Revision != s.snapshot.Revision || v.CoreEpoch != s.snapshot.CoreEpoch {
		s.retained = nil
	}
	retained := make(map[string]map[string]time.Time, len(v.Accounts))
	for _, account := range v.Accounts {
		// An account whose sample did not measure addresses has none to keep.
		if !account.AddressesMeasured {
			continue
		}
		keys := make(map[string]time.Time, len(account.AddressKeys))
		for _, key := range account.AddressKeys {
			keys[key] = now
		}
		for key, seen := range s.retained[account.CredentialID] {
			if len(keys) >= maxAddressKeys {
				break
			}
			if _, latest := keys[key]; !latest && now.Sub(seen) < AddressRetentionWindow {
				keys[key] = seen
			}
		}
		retained[account.CredentialID] = keys
	}
	s.retained = retained
	s.snapshot = copySnapshot(v)
	s.received = now
}

// Snapshot returns the latest sample with every measured account's addresses
// seen within the retention window. Reading does not consume them.
func (s *Store) Snapshot() (traffic.ConnectionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.received.IsZero() || now.Sub(s.received) >= snapshotFreshness {
		return traffic.ConnectionSnapshot{}, errors.New("device observation unavailable")
	}
	snapshot := copySnapshot(s.snapshot)
	for i := range snapshot.Accounts {
		account := &snapshot.Accounts[i]
		account.AddressKeys = nil
		if !account.AddressesMeasured {
			continue
		}
		for key, seen := range s.retained[account.CredentialID] {
			if now.Sub(seen) < AddressRetentionWindow {
				account.AddressKeys = append(account.AddressKeys, key)
			}
		}
		sort.Strings(account.AddressKeys)
	}
	return snapshot, nil
}

// Package connections keeps the panel's live view of per-node connection
// counts in memory. The panel is the only writer of its own database, and a
// live count is meaningless after a restart, so it is deliberately never
// persisted: a node that stops reporting leaves no stale row behind.
package connections

import (
	"errors"
	"sync"
	"time"

	"proxycontrol/pkg/contracts/traffic"
)

// Store holds the latest connection snapshot reported by each node. A snapshot
// counts as current for ttl after it is received; an older one is treated as
// unknown so a node that goes silent never leaves an invented count behind.
type Store struct {
	// Now is optional and exists for tests; it defaults to time.Now.
	Now func() time.Time

	ttl   time.Duration
	mu    sync.Mutex
	nodes map[string]entry
	gates map[string]bool
}

// GateSupport tracks this control session's negotiated support, not an old
// capability record left in SQLite by a previously installed Agent.
func (s *Store) SetGateSupport(nodeID string, supported bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gates == nil {
		s.gates = map[string]bool{}
	}
	s.gates[nodeID] = supported
}
func (s *Store) GateSupport(nodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gates[nodeID]
}

type entry struct {
	snapshot traffic.ConnectionSnapshot
	received time.Time
}

// ErrNodeMismatch reports a snapshot whose node ID is not the one reporting it.
var ErrNodeMismatch = errors.New("connection snapshot node mismatch")

// NewStore returns a store whose entries expire after ttl. A non-positive ttl
// means every snapshot is immediately stale.
func NewStore(ttl time.Duration) *Store {
	return &Store{ttl: ttl, nodes: map[string]entry{}}
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Record stores the latest snapshot for nodeID. It rejects a snapshot that
// fails validation or that claims a different node, so a node can never report
// another node's connections.
func (s *Store) Record(nodeID string, snapshot traffic.ConnectionSnapshot) error {
	if nodeID == "" || snapshot.NodeID != nodeID {
		return ErrNodeMismatch
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nodes == nil {
		s.nodes = map[string]entry{}
	}
	s.nodes[nodeID] = entry{snapshot: snapshot, received: s.now()}
	return nil
}

// Snapshot returns the current snapshot for a node, if one is fresh.
func (s *Store) Snapshot(nodeID string) (traffic.ConnectionSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.nodes[nodeID]
	if !ok || s.stale(current.received) {
		return traffic.ConnectionSnapshot{}, false
	}
	return current.snapshot, true
}

// Snapshots returns every fresh snapshot.
func (s *Store) Snapshots() []traffic.ConnectionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]traffic.ConnectionSnapshot, 0, len(s.nodes))
	for _, current := range s.nodes {
		if !s.stale(current.received) {
			result = append(result, current.snapshot)
		}
	}
	return result
}

func (s *Store) stale(received time.Time) bool {
	return s.now().Sub(received) >= s.ttl
}

// Sum adds up the connections of the given credentials across snapshots. A
// credential absent from the set is ignored.
func Sum(snapshots []traffic.ConnectionSnapshot, credentialIDs map[string]bool) uint64 {
	var total uint64
	for _, snapshot := range snapshots {
		for _, account := range snapshot.Accounts {
			if credentialIDs[account.CredentialID] {
				total += account.Connections
			}
		}
	}
	return total
}

// Package observer holds volatile core observations between five-second polls
// and fifteen-second control heartbeats. Nothing in this package writes to disk.
package observer

import (
	"errors"
	"proxycontrol/pkg/contracts/traffic"
	"sync"
	"time"
)

type Store struct {
	mu       sync.Mutex
	snapshot traffic.ConnectionSnapshot
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
func (s *Store) Record(v traffic.ConnectionSnapshot, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil || v.Validate() != nil {
		s.snapshot = traffic.ConnectionSnapshot{}
		s.received = time.Time{}
		return
	}
	s.snapshot = copySnapshot(v)
	s.received = s.now()
}
func (s *Store) Snapshot() (traffic.ConnectionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.received.IsZero() || s.now().Sub(s.received) >= 10*time.Second {
		return traffic.ConnectionSnapshot{}, errors.New("device observation unavailable")
	}
	return copySnapshot(s.snapshot), nil
}

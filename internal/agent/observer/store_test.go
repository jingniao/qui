package observer

import (
	"errors"
	"proxycontrol/pkg/contracts/traffic"
	"strings"
	"sync"
	"testing"
	"time"
)

// observed builds a valid snapshot whose total is the sum of its accounts.
func observed(at time.Time, accounts ...traffic.ConnectionAccount) traffic.ConnectionSnapshot {
	var total uint64
	for _, account := range accounts {
		total += account.Connections
	}
	return traffic.ConnectionSnapshot{ObservationContractVersion: 2, NodeID: "node", Revision: 1, CoreEpoch: "epoch", ObservedAt: at, Scope: traffic.ConnectionScope, Connections: total, Accounts: accounts}
}

func measured(credential string, connections uint64, keys ...string) traffic.ConnectionAccount {
	return traffic.ConnectionAccount{CredentialID: credential, Connections: connections, AddressKeys: keys, AddressesMeasured: true}
}

func TestObservationExpiryCopiesAndConcurrentReads(t *testing.T) {
	now := time.Unix(100, 0)
	s := &Store{Now: func() time.Time { return now }}
	v := traffic.ConnectionSnapshot{ObservationContractVersion: 2, NodeID: "n", Revision: 1, CoreEpoch: "e", ObservedAt: now, Scope: traffic.ConnectionScope, Accounts: []traffic.ConnectionAccount{{CredentialID: "c", AddressesMeasured: true}}}
	s.Record(v, nil)
	v.Accounts[0].CredentialID = "mutated"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				got, err := s.Snapshot()
				if err != nil || len(got.Accounts) != 1 || got.Accounts[0].CredentialID != "c" {
					t.Error("observation not isolated")
					return
				}
				got.Accounts[0].CredentialID = "mutated"
			}
		}()
	}
	wg.Wait()
	now = now.Add(10 * time.Second)
	if _, err := s.Snapshot(); err == nil {
		t.Fatal("stale observation retained")
	}
}

// A source one sample caught stays visible while the retention window lasts, and
// the counts of every read come from the latest sample.
func TestRetainedAddressSurvivesAnEmptySampleWithinTheWindow(t *testing.T) {
	now := time.Unix(100, 0)
	s := &Store{Now: func() time.Time { return now }}
	s.Record(observed(now, measured("a", 1, "10.0.0.1")), nil)
	now = now.Add(2 * time.Second)
	s.Record(observed(now, measured("a", 5)), nil)
	for i := 0; i < 3; i++ {
		got, err := s.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("read %d: snapshot is invalid: %v", i, err)
		}
		if len(got.Accounts) != 1 || strings.Join(got.Accounts[0].AddressKeys, ",") != "10.0.0.1" || got.Accounts[0].Connections != 5 {
			t.Fatalf("read %d: retained address or latest count missing: %v", i, got)
		}
	}
}

// Reading the snapshot neither removes nor prolongs a retained source.
func TestRetainedAddressExpiresAfterTheWindow(t *testing.T) {
	start := time.Unix(100, 0)
	now := start
	s := &Store{Now: func() time.Time { return now }}
	s.Record(observed(now, measured("a", 1, "10.0.0.1")), nil)
	for elapsed := 5 * time.Second; elapsed <= AddressRetentionWindow; elapsed += 5 * time.Second {
		now = start.Add(elapsed)
		s.Record(observed(now, measured("a", 1)), nil)
		got, err := s.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if kept := len(got.Accounts[0].AddressKeys) == 1; kept != (elapsed < AddressRetentionWindow) {
			t.Fatalf("after %s the address was kept=%v: %v", elapsed, kept, got.Accounts[0])
		}
	}
}

// A failed sample and a new process identity both start over: the sources the
// previous one saw are not live any more.
func TestFailedSampleAndIdentityChangeClearRetainedAddresses(t *testing.T) {
	now := time.Unix(100, 0)
	s := &Store{Now: func() time.Time { return now }}
	s.Record(observed(now, measured("a", 1, "10.0.0.1")), nil)
	s.Record(traffic.ConnectionSnapshot{}, errors.New("observation failed"))
	if _, err := s.Snapshot(); err == nil {
		t.Fatal("failed sample left the observation available")
	}
	s.Record(observed(now, measured("a", 1)), nil)
	if got, err := s.Snapshot(); err != nil || len(got.Accounts[0].AddressKeys) != 0 {
		t.Fatalf("a failed sample kept an address: %v %v", got, err)
	}
	for _, identity := range []struct {
		name   string
		change func(*traffic.ConnectionSnapshot)
	}{
		{"node", func(v *traffic.ConnectionSnapshot) { v.NodeID = "other" }},
		{"revision", func(v *traffic.ConnectionSnapshot) { v.Revision = 2 }},
		{"epoch", func(v *traffic.ConnectionSnapshot) { v.CoreEpoch = "next" }},
	} {
		t.Run(identity.name, func(t *testing.T) {
			now = time.Unix(200, 0)
			s.Record(observed(now, measured("a", 1, "10.0.0.1")), nil)
			now = now.Add(time.Second)
			next := observed(now, measured("a", 1))
			identity.change(&next)
			s.Record(next, nil)
			if got, err := s.Snapshot(); err != nil || len(got.Accounts[0].AddressKeys) != 0 {
				t.Fatalf("a new identity kept an address: %v %v", got, err)
			}
		})
	}
}

// Addresses that were not measured cannot carry keys, and a credential the
// latest sample no longer reports is dropped with them.
func TestRetainedAddressesFollowTheLatestSample(t *testing.T) {
	now := time.Unix(100, 0)
	s := &Store{Now: func() time.Time { return now }}
	s.Record(observed(now, measured("a", 1, "10.0.0.1"), measured("b", 1, "10.0.0.2")), nil)
	now = now.Add(time.Second)
	s.Record(observed(now, traffic.ConnectionAccount{CredentialID: "a", Connections: 1}, measured("c", 1)), nil)
	got, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("snapshot is invalid: %v", err)
	}
	if len(got.Accounts) != 2 || len(got.Accounts[0].AddressKeys) != 0 {
		t.Fatalf("unmeasured or dropped account received a retained address: %v", got)
	}
	now = now.Add(time.Second)
	s.Record(observed(now, measured("a", 1), measured("b", 1), measured("c", 1)), nil)
	got, err = s.Snapshot()
	if err != nil || len(got.Accounts) != 3 || len(got.Accounts[0].AddressKeys) != 0 || len(got.Accounts[1].AddressKeys) != 0 {
		t.Fatalf("a credential missing from a sample kept its addresses: %v %v", got, err)
	}
}

package observer

import (
	"proxycontrol/pkg/contracts/traffic"
	"sync"
	"testing"
	"time"
)

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

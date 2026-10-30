package connections

import (
	"proxycontrol/pkg/contracts/traffic"
	"testing"
	"time"
)

func TestCoreSnapshotsMergeAddressesAndExpireTogether(t *testing.T) {
	s := NewStore(45 * time.Second)
	now := time.Now()
	s.Now = func() time.Time { return now }
	x := snapshot("n", "cred", 2)
	x.DeviceGateSupported = true
	x.ObservationContractVersion = 2
	x.Accounts[0].AddressesMeasured = true
	x.Accounts[0].AddressKeys = []string{"192.0.2.1", "2001:db8::/64"}
	b := x
	b.Accounts = append([]traffic.ConnectionAccount(nil), x.Accounts...)
	b.Accounts[0].AddressKeys = []string{"192.0.2.1", "192.0.2.2"}
	b.CoreEpoch = "peer"
	if err := s.RecordCores("n", map[string]traffic.ConnectionSnapshot{"xray": x, "sing-box": b}); err != nil {
		t.Fatal(err)
	}
	merged, ok := s.Snapshot("n")
	if !ok || merged.Connections != 4 || len(merged.Accounts) != 1 || len(merged.Accounts[0].AddressKeys) != 3 {
		t.Fatal(merged, ok)
	}
	if len(s.CoreSnapshots("n")) != 2 {
		t.Fatal("missing core snapshots")
	}
	now = now.Add(45 * time.Second)
	if _, ok := s.Snapshot("n"); ok || len(s.CoreSnapshots("n")) != 0 {
		t.Fatal("core snapshots did not expire")
	}
	// A partial replacement removes stale peer observations without losing the measured core.
	if err := s.RecordCores("n", map[string]traffic.ConnectionSnapshot{"xray": x}); err != nil {
		t.Fatal(err)
	}
	if len(s.CoreSnapshots("n")) != 1 {
		t.Fatal("stale peer retained")
	}
}

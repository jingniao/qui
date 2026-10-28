package xray

import (
	"context"
	"testing"
)

func TestAddressObservationXrayCanonicalKeysAndUnknownFallback(t *testing.T) {
	fixture := &onlineStatsFixture{online: map[string]int64{"user>>>credential:device-a>>>online": 3}, ips: map[string]map[string]int64{"user>>>credential:device-a>>>online": {"198.51.100.12": 1, "2001:db8:1:2::1": 1, "2001:db8:1:2::2": 1}}}
	a := onlineAdapter(registerOnlineStatsFixture(t, fixture))
	s, err := a.ConnectionSnapshot(context.Background())
	if err != nil || !s.DeviceGateSupported || s.ObservationContractVersion != 2 || len(s.Accounts) != 2 {
		t.Fatal("address snapshot failed", err)
	}
	for _, account := range s.Accounts {
		if account.CredentialID == "device-a" && (len(account.AddressKeys) != 2 || account.AddressKeys[0] != "198.51.100.12" || account.AddressKeys[1] != "2001:db8:1:2::/64") {
			t.Fatal("address normalization failed")
		}
		if !account.AddressesMeasured {
			t.Fatal("known empty user became unknown")
		}
	}
	fixture.mu.Lock()
	fixture.ips = nil
	fixture.mu.Unlock()
	s, err = a.ConnectionSnapshot(context.Background())
	if err != nil || s.DeviceGateSupported {
		t.Fatal("missing list capability was not explicit")
	}
	for _, account := range s.Accounts {
		if account.AddressesMeasured {
			t.Fatal("unknown address list fabricated")
		}
	}
}

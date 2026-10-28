package user

import (
	"proxycontrol/pkg/contracts/desiredstate"
	"sync"
	"testing"
	"time"
)

func TestDeviceObserverExcludesRelayUnboundAndWireGuardCredentials(t *testing.T) {
	s := desiredstate.State{Inbounds: []desiredstate.Inbound{{ID: "entry", Enabled: true, Protocol: "vless", CredentialIDs: []string{"entry", "relay"}}, {ID: "wg", Enabled: true, Protocol: "wireguard", CredentialIDs: []string{"wg"}}}, Credentials: []desiredstate.Credential{{ID: "entry", UserID: "u", Protocol: "vless"}, {ID: "relay", UserID: "u", Protocol: "vless", Internal: true}, {ID: "unbound", UserID: "u", Protocol: "vless"}, {ID: "wg", UserID: "u", Protocol: "wireguard"}}}
	owned := entryCredentials(s)
	if len(owned) != 1 || owned["entry"] != "u" {
		t.Fatal("non-entry identity entered admission")
	}
}
func TestDeviceObserverHourlyAlertContainsOnlyUserIdentity(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := 0
	var mu sync.Mutex
	o := &DeviceObserver{Now: func() time.Time { return now }, Publish: func(event string, payload any) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		fields, ok := payload.(map[string]any)
		if event != "system.alert" || !ok || len(fields) != 3 || fields["category"] != "user_access" || fields["userId"] != "u" {
			t.Error("alert contains unexpected material")
		}
	}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); o.alert("u") }()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatal("duplicate alert in one hour")
	}
	now = now.Add(time.Hour)
	o.alert("u")
	if calls != 2 {
		t.Fatal("next hourly alert suppressed")
	}
}

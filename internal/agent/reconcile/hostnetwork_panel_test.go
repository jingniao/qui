package reconcile

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/agent/hostnet"
	"proxycontrol/pkg/contracts/desiredstate"
)

// fakeHostNetworkDialFrom replaces the source-bound reachability proof.
func fakeHostNetworkDialFrom(t *testing.T, dial func(ctx context.Context, network, source, address string) error) {
	t.Helper()
	previous := hostNetworkDialFrom
	hostNetworkDialFrom = dial
	t.Cleanup(func() { hostNetworkDialFrom = previous })
}

// TestHostNetworkPanelExempt: a helper that declares panel_exempt receives the
// panel destinations and the proof dials from the enforced address; an older
// helper keeps the previous file and the unbound proof.
func TestHostNetworkPanelExempt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		feature   bool
		failProof bool
	}{
		{name: "supported", feature: true},
		{name: "supported proof fails", feature: true, failProof: true},
		{name: "old helper"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowHostNetwork(t)
			var plain []string
			var bound []string
			fakeHostNetworkDial(t, func(_ context.Context, network, address string) error {
				plain = append(plain, network+" "+address)
				return nil
			})
			fakeHostNetworkDialFrom(t, func(_ context.Context, network, source, address string) error {
				bound = append(bound, network+" "+source+" "+address)
				if tc.failProof {
					return errors.New("no route")
				}
				return nil
			})
			dir := t.TempDir()
			status := hostnet.Status{ID: "previous", OK: true, Features: []string{hostnet.FeatureExtraExempt}}
			if tc.feature {
				status.Features = append(status.Features, hostnet.FeaturePanelExempt)
			}
			if err := hostnet.WriteStatus(dir, status); err != nil {
				t.Fatal(err)
			}
			startFakeHostNetworkHelper(t, dir, func(hostnet.Desired) (bool, string) { return true, "" })
			state := desiredstate.State{Network: &desiredstate.Network{HostSourceIPv4: "198.51.100.1"}}
			err := syncingManager(t, dir, "127.0.0.1:443").applyHostNetwork(context.Background(), state)
			if tc.failProof {
				if err == nil || !strings.Contains(err.Error(), "from the enforced source 198.51.100.1") {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			desired, err := hostnet.ReadDesired(dir)
			if err != nil {
				t.Fatal(err)
			}
			if tc.feature {
				if !reflect.DeepEqual(desired.PanelIPv4, []string{"127.0.0.1"}) || len(desired.PanelIPv6) != 0 {
					t.Fatalf("panel destinations: %+v", desired)
				}
				if len(plain) != 0 || !reflect.DeepEqual(bound, []string{"tcp4 198.51.100.1 127.0.0.1:443"}) {
					t.Fatalf("proof plain=%v bound=%v", plain, bound)
				}
				return
			}
			if len(desired.PanelIPv4)+len(desired.PanelIPv6) != 0 {
				t.Fatalf("old helper received panel destinations: %+v", desired)
			}
			if len(bound) != 0 || !reflect.DeepEqual(plain, []string{"tcp4 127.0.0.1:443"}) {
				t.Fatalf("proof plain=%v bound=%v", plain, bound)
			}
		})
	}
}

func TestControlAddresses(t *testing.T) {
	for _, tc := range []struct {
		address    string
		ipv4, ipv6 []string
		wantError  bool
	}{
		{address: "203.0.113.5:18880", ipv4: []string{"203.0.113.5"}},
		{address: "[2001:db8::5]:18880", ipv6: []string{"2001:db8::5"}},
		{address: "[::ffff:203.0.113.5]:18880", ipv4: []string{"203.0.113.5"}},
		{address: "203.0.113.5", wantError: true},
	} {
		t.Run(tc.address, func(t *testing.T) {
			ipv4, ipv6, err := controlAddresses(context.Background(), tc.address)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil || !reflect.DeepEqual(ipv4, tc.ipv4) || !reflect.DeepEqual(ipv6, tc.ipv6) {
				t.Fatalf("got %v %v %v", ipv4, ipv6, err)
			}
		})
	}
}

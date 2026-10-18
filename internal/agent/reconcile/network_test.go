package reconcile

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestNetworkAvailabilityCheckCases(t *testing.T) {
	names := map[string]bool{"eth0": true, "eth1": true}
	addresses := map[string][]net.IP{
		"eth0": {net.ParseIP("192.0.2.10"), net.ParseIP("2001:db8::10")},
		"eth1": {net.ParseIP("198.51.100.20")},
	}
	cases := []struct {
		name    string
		network desiredstate.Network
		valid   bool
	}{
		{name: "unconfigured", network: desiredstate.Network{}, valid: true},
		{name: "interface only", network: desiredstate.Network{OutboundInterface: "eth0"}, valid: true},
		{name: "address only", network: desiredstate.Network{OutboundSourceIP: "192.0.2.10"}, valid: true},
		{name: "address on interface", network: desiredstate.Network{OutboundSourceIP: "2001:db8::10", OutboundInterface: "eth0"}, valid: true},
		{name: "missing interface", network: desiredstate.Network{OutboundInterface: "eth9"}, valid: false},
		{name: "missing address", network: desiredstate.Network{OutboundSourceIP: "203.0.113.7"}, valid: false},
		{name: "address on the wrong interface", network: desiredstate.Network{OutboundSourceIP: "192.0.2.10", OutboundInterface: "eth1"}, valid: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := checkNetworkAvailable(test.network, names, addresses)
			if test.valid && err != nil {
				t.Fatalf("valid network rejected: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("unavailable network accepted")
			}
		})
	}
}

// TestNetworkUnavailableIsTemporaryAndRetried pins the two properties the
// feature depends on: the failure is reported under its own code, and it is not
// cached, so the same revision succeeds once the address appears.
func TestNetworkUnavailableIsTemporaryAndRetried(t *testing.T) {
	if permanentFailure(networkUnavailableCode) {
		t.Fatal("network_unavailable must not be cached as a permanent failure")
	}
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	available := false
	manager.NetworkAvailable = func(desiredstate.Network) error {
		if available {
			return nil
		}
		return errors.New("outbound source IP 192.0.2.10 is not assigned to any local interface")
	}
	target := state(freePort(t), 1)
	target.Network = &desiredstate.Network{OutboundSourceIP: "192.0.2.10", OutboundInterface: "eth0"}
	result, err := manager.Apply(ctx, target)
	if err == nil || result.Code != networkUnavailableCode {
		t.Fatalf("missing address did not report a temporary failure: %+v %v", result, err)
	}
	if adapter.renderCalls != 0 {
		t.Fatal("an unavailable network reached the core renderer")
	}
	if _, err := os.Stat(filepath.Join(directory, "revisions", "1", "result.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary network failure was cached: %v", err)
	}
	available = true
	result, err = manager.Apply(ctx, target)
	if err != nil || !result.Success {
		t.Fatalf("retry after the address appeared failed: %+v %v", result, err)
	}
	if manager.Current() != 1 {
		t.Fatal("retried revision was not committed")
	}
}

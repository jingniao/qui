package reconcile

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/singbox"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
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
	for _, test := range []struct {
		name      string
		core      string
		target    string
		protocol  string
		enabled   bool
		active    bool
		available bool
	}{
		{name: "Xray explicit binding", core: "xray", target: "direct", protocol: "direct", enabled: true, active: true},
		{name: "sing-box explicit binding", core: "sing-box", target: "direct", protocol: "direct", enabled: true, active: true},
		{name: "reachable proxy", core: "sing-box", target: "other", protocol: "socks5", enabled: true, active: true},
		{name: "unreachable proxy", core: "xray", target: "direct", protocol: "socks5", enabled: true, active: true, available: true},
		{name: "disabled outbound", core: "xray", target: "direct", protocol: "socks5", active: true, available: true},
		{name: "inactive core", core: "sing-box", target: "direct", protocol: "direct", enabled: true, available: true},
		{name: "GOST ignores source fields", core: "gost", target: "direct", protocol: "direct", enabled: true, active: true, available: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			multi, err := core.NewMultiRuntime(&xray.Adapter{}, &singbox.Adapter{})
			if err != nil {
				t.Fatal(err)
			}
			manager := &Reconciler{Adapter: multi, NetworkAvailable: func(network desiredstate.Network) error {
				return checkNetworkAvailable(network, names, addresses)
			}}
			target := state(12345, 1)
			target.Inbounds[0].Core = test.core
			target.Inbounds[0].DefaultOutbound = test.target
			target.Inbounds[0].Enabled = test.active
			bound := desiredstate.Outbound{ID: "other", Protocol: test.protocol, Enabled: test.enabled, SourceIP: "192.0.2.99"}
			if test.protocol == "direct" {
				bound.ID = "direct"
				target.Outbounds = []desiredstate.Outbound{bound}
			} else {
				target.Outbounds = append(target.Outbounds, bound)
			}
			err = manager.verifyOutboundNetworks(target)
			if (err == nil) != test.available {
				t.Fatalf("effective multi-core binding availability: %v", err)
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
	for _, test := range []struct {
		name    string
		network *desiredstate.Network
		source  desiredstate.Outbound
		valid   bool
	}{
		{name: "explicit missing address", source: desiredstate.Outbound{SourceIP: "192.0.2.99"}},
		{name: "explicit missing interface", source: desiredstate.Outbound{SourceInterface: "eth9"}},
		{name: "explicit IPv6 missing address", source: desiredstate.Outbound{SourceIP: "2001:db8::99"}},
		{name: "inherited interface mismatch", network: &desiredstate.Network{OutboundInterface: "eth0"}, source: desiredstate.Outbound{SourceIP: "198.51.100.20"}},
		{name: "inherited address mismatch", network: &desiredstate.Network{OutboundSourceIP: "192.0.2.10"}, source: desiredstate.Outbound{SourceInterface: "eth1"}},
		{name: "explicit overrides", network: &desiredstate.Network{OutboundSourceIP: "192.0.2.10", OutboundInterface: "eth0"}, source: desiredstate.Outbound{SourceIP: "198.51.100.20", SourceInterface: "eth1"}, valid: true},
		{name: "mapped local address", source: desiredstate.Outbound{SourceIP: "::ffff:192.0.2.10"}, valid: true},
		{name: "disabled missing address", source: desiredstate.Outbound{ID: "disabled", SourceIP: "192.0.2.99"}, valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := &fakeCoreAdapter{}
			manager, err := New(t.TempDir(), "node-test", adapter)
			if err != nil {
				t.Fatal(err)
			}
			manager.NetworkAvailable = func(network desiredstate.Network) error {
				return checkNetworkAvailable(network, map[string]bool{"eth0": true, "eth1": true}, map[string][]net.IP{
					"eth0": {net.ParseIP("192.0.2.10"), net.ParseIP("2001:db8::10")},
					"eth1": {net.ParseIP("198.51.100.20")},
				})
			}
			baseline := state(freePort(t), 1)
			if result, err := manager.Apply(ctx, baseline); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			target := state(baseline.Inbounds[0].Port, 2)
			target.Network = test.network
			if test.source.ID == "disabled" {
				test.source.Protocol = "direct"
				target.Outbounds = append(target.Outbounds, test.source)
			} else {
				target.Outbounds[0].SourceIP = test.source.SourceIP
				target.Outbounds[0].SourceInterface = test.source.SourceInterface
			}
			result, err := manager.Apply(ctx, target)
			if test.valid {
				if err != nil || !result.Success {
					t.Fatalf("valid effective binding failed: %+v %v", result, err)
				}
				return
			}
			if err == nil || result.Code != networkUnavailableCode || !strings.Contains(result.Message, "outbound direct:") {
				t.Fatalf("invalid outbound binding was not explained: %+v %v", result, err)
			}
			if adapter.renderCalls != 1 || adapter.applyCalls != 1 || manager.Current() != 1 {
				t.Fatal("invalid binding touched the last successful runtime")
			}
			if _, err := os.Stat(filepath.Join(manager.Directory, "revisions", "2")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unavailable outbound revision was persisted: %v", err)
			}
		})
	}
}

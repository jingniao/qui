package core

import (
	"reflect"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
)

func TestGroupProbesKeepEverySourceAndMember(t *testing.T) {
	state := desiredstate.State{
		Inbounds: []desiredstate.Inbound{{Enabled: true, EgressSourceIP: "192.0.2.20"}, {Enabled: true, EgressSourceIP: "192.0.2.10"}, {Enabled: true, EgressSourceIP: "192.0.2.10"}, {Enabled: false, EgressSourceIP: "192.0.2.30"}},
		OutboundGroups: []desiredstate.OutboundGroup{
			{ProbeURL: "https://probe.example", ProbeInterval: 30, Members: []desiredstate.GroupMember{{OutboundID: "p"}, {OutboundID: "q"}}},
			{ProbeURL: "https://probe.example", ProbeInterval: 10, Members: []desiredstate.GroupMember{{OutboundID: "p"}}},
		},
		Outbounds: []desiredstate.Outbound{{ID: "p"}, {ID: "q"}},
	}
	probes, err := GroupProbes(state)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, probe := range probes {
		keys = append(keys, probe.Key())
		if probe.OutboundID == "p" && probe.Interval != 10*time.Second {
			t.Fatalf("probe interval = %s", probe.Interval)
		}
	}
	want := []string{"p", "q", "192.0.2.10|p", "192.0.2.10|q", "192.0.2.20|p", "192.0.2.20|q"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("probe identities = %v, want %v", keys, want)
	}
	defaultHealth := scheduler.Health{Healthy: true, CheckedAt: time.Now()}
	health := map[string]scheduler.Health{"p": defaultHealth, ProbeKey("192.0.2.10", "q"): defaultHealth}
	view := SourceHealth(state, health, "192.0.2.10")
	if _, exists := view["p"]; exists {
		t.Fatal("default source observation leaked into an unobserved source")
	}
	if !view["q"].Healthy {
		t.Fatal("source observation missing")
	}
	state.OutboundGroups[1].ProbeURL = "https://conflict.example"
	if _, err := GroupProbes(state); err == nil {
		t.Fatal("conflicting probe URLs accepted")
	}
}

func TestProbeFingerprintChangesWithSourceNetworkAndPeerCredential(t *testing.T) {
	state := desiredstate.State{
		Network:     &desiredstate.Network{OutboundSourceIP: "192.0.2.1", OutboundInterface: "eth0"},
		Outbounds:   []desiredstate.Outbound{{ID: "wg", Protocol: "wireguard", Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{Peers: []desiredstate.WireGuardPeer{{CredentialID: "peer"}}}}}},
		Credentials: []desiredstate.Credential{{ID: "peer", Secret: "first"}},
	}
	probe := Probe{OutboundID: "wg", URL: "https://probe.example"}
	fingerprint := func() string {
		value, err := ProbeFingerprint(state, probe)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	first := fingerprint()
	state.Network.OutboundSourceIP = "192.0.2.2"
	if fingerprint() == first {
		t.Fatal("effective network change reused a stale observation")
	}
	probe.SourceIP = "192.0.2.10"
	scoped := fingerprint()
	if scoped == first {
		t.Fatal("source identity missing from fingerprint")
	}
	state.Network.OutboundInterface = "eth1"
	state.Network.OutboundSourceIP = "192.0.2.3"
	if fingerprint() != scoped {
		t.Fatal("overridden node binding changed a source-specific path")
	}
	state.Credentials[0].Secret = "second"
	if fingerprint() == scoped {
		t.Fatal("WireGuard peer credential change reused a stale observation")
	}
}

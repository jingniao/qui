package reconcile

import (
	"context"
	"reflect"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

// TestSourceTargetsFollowTheBindingsApplyChecks pins which bindings the
// heartbeat check watches: the host sources always, the core-related ones only
// when a core is rendered, and only enabled, non-block, bound outbounds.
func TestSourceTargetsFollowTheBindingsApplyChecks(t *testing.T) {
	state := desiredstate.State{
		Network: &desiredstate.Network{HostSourceIPv4: "192.0.2.1", HostSourceIPv6: "2001:db8::1", OutboundSourceIP: "192.0.2.2", OutboundInterface: "eth0"},
		Inbounds: []desiredstate.Inbound{
			{ID: "in-on", Enabled: true, EgressSourceIP: "2001:db8::2"},
			{ID: "in-off", EgressSourceIP: "2001:db8::3"},
			{ID: "in-plain", Enabled: true},
		},
		Outbounds: []desiredstate.Outbound{
			{ID: "out-ip", Protocol: "direct", Enabled: true, SourceIP: "192.0.2.3"},
			{ID: "out-iface", Protocol: "direct", Enabled: true, SourceInterface: "wg0"},
			{ID: "out-block", Protocol: "block", Enabled: true, SourceIP: "192.0.2.4"},
			{ID: "out-off", Protocol: "direct", SourceIP: "192.0.2.5"},
			{ID: "out-plain", Protocol: "direct", Enabled: true},
		},
	}
	host := []sourceTarget{
		{kind: SourceKindHost, network: desiredstate.Network{OutboundSourceIP: "192.0.2.1"}},
		{kind: SourceKindHost, network: desiredstate.Network{OutboundSourceIP: "2001:db8::1"}},
	}
	want := append(append([]sourceTarget{}, host...),
		sourceTarget{kind: SourceKindCore, network: desiredstate.Network{OutboundSourceIP: "192.0.2.2", OutboundInterface: "eth0"}},
		sourceTarget{kind: SourceKindInbound, owner: "in-on", network: desiredstate.Network{OutboundSourceIP: "2001:db8::2"}},
		sourceTarget{kind: SourceKindOutbound, owner: "out-ip", network: desiredstate.Network{OutboundSourceIP: "192.0.2.3", OutboundInterface: "eth0"}},
		sourceTarget{kind: SourceKindOutbound, owner: "out-iface", network: desiredstate.Network{OutboundSourceIP: "192.0.2.2", OutboundInterface: "wg0"}},
	)
	if got := sourceTargets(state, true, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("targets with a core:\n got %+v\nwant %+v", got, want)
	}
	if got := sourceTargets(state, false, false); !reflect.DeepEqual(got, host) {
		t.Fatalf("targets without a core: %+v", got)
	}
	if got := sourceTargets(desiredstate.State{}, true, false); len(got) != 0 {
		t.Fatalf("empty state targets: %+v", got)
	}
}

// TestMissingSourcesReportsOnlyAbsentBindings checks the host comparison with
// the real interfaces: the loopback address exists on every test host, the
// documentation address does not.
func TestMissingSourcesReportsOnlyAbsentBindings(t *testing.T) {
	r := &Reconciler{manifest: Manifest{Revision: 5, Digest: "cached"}, sourceDigest: "cached", sourceTargets: []sourceTarget{
		{kind: SourceKindHost, network: desiredstate.Network{OutboundSourceIP: "127.0.0.1"}},
		{kind: SourceKindInbound, owner: "in-1", network: desiredstate.Network{OutboundSourceIP: "192.0.2.77"}},
		{kind: SourceKindCore, network: desiredstate.Network{OutboundInterface: "qui-no-such-link"}},
	}}
	revision, issues, err := r.MissingSources()
	if err != nil {
		t.Fatal(err)
	}
	want := []SourceIssue{
		{Kind: SourceKindInbound, OwnerID: "in-1", Address: "192.0.2.77"},
		{Kind: SourceKindCore, Interface: "qui-no-such-link"},
	}
	if revision != 5 || !reflect.DeepEqual(issues, want) {
		t.Fatalf("revision %d issues %+v", revision, issues)
	}
}

// TestMissingSourcesFollowsTheRunningRevision reads the bindings from the
// retained revision and refreshes them when a new revision is committed.
func TestMissingSourcesFollowsTheRunningRevision(t *testing.T) {
	manager, err := New(t.TempDir(), "node-test", &fakeCoreAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	if revision, issues, err := manager.MissingSources(); err != nil || revision != 0 || len(issues) != 0 {
		t.Fatalf("no revision: %d %+v %v", revision, issues, err)
	}
	manager.NetworkAvailable = func(desiredstate.Network) error { return nil }
	port := freePort(t)
	first := state(port, 1)
	first.Network = &desiredstate.Network{OutboundSourceIP: "192.0.2.10"}
	if result, err := manager.Apply(context.Background(), first); err != nil || !result.Success {
		t.Fatalf("apply first: %+v %v", result, err)
	}
	revision, issues, err := manager.MissingSources()
	if err != nil || revision != 1 || !reflect.DeepEqual(issues, []SourceIssue{{Kind: SourceKindCore, Address: "192.0.2.10"}}) {
		t.Fatalf("first revision: %d %+v %v", revision, issues, err)
	}
	if result, err := manager.Apply(context.Background(), state(port, 2)); err != nil || !result.Success {
		t.Fatalf("apply second: %+v %v", result, err)
	}
	if revision, issues, err := manager.MissingSources(); err != nil || revision != 2 || len(issues) != 0 {
		t.Fatalf("second revision: %d %+v %v", revision, issues, err)
	}
}

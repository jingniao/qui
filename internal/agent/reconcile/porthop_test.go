package reconcile

import (
	"context"
	"testing"

	"proxycontrol/internal/agent/hostnet"
	"proxycontrol/pkg/contracts/desiredstate"
)

func hopState(port, start, end int, enabled bool) desiredstate.State {
	return desiredstate.State{Inbounds: []desiredstate.Inbound{{
		Protocol: "hysteria2", Enabled: enabled, Port: port,
		Settings: desiredstate.ProtocolSettings{QUIC: &desiredstate.QUICSettings{HopPortStart: start, HopPortEnd: end, HopIntervalSeconds: 30}},
	}}}
}

func TestCollectPortHopsOnlyEnabledHysteria2Inbounds(t *testing.T) {
	if hops := collectPortHops(hopState(8443, 20000, 20010, true).Inbounds); len(hops) != 1 || hops[0] != (hostnet.PortHop{Start: 20000, End: 20010, Target: 8443}) {
		t.Fatalf("hop range was not mirrored: %+v", hops)
	}
	for _, state := range []desiredstate.State{
		hopState(8443, 20000, 20010, false),
		{Inbounds: []desiredstate.Inbound{{Protocol: "tuic", Enabled: true, Settings: desiredstate.ProtocolSettings{QUIC: &desiredstate.QUICSettings{HopPortStart: 20000, HopPortEnd: 20010}}}}},
		{},
	} {
		if hops := collectPortHops(state.Inbounds); len(hops) != 0 {
			t.Fatalf("a state without an enabled Hysteria2 hop produced %+v", hops)
		}
	}
}

// The helper's desired file mirrors the hop ranges of the revision being
// applied; a later revision without hops writes a clear request so the table
// is removed, and the panel's capability gate keeps an old Agent out.
func TestApplyHostNetworkMirrorsAndClearsHops(t *testing.T) {
	directory := t.TempDir()
	manager := syncingManager(t, directory, "127.0.0.1:9443")
	allowHostNetwork(t)
	shortHostNetworkWait(t)
	fakeHostNetworkDial(t, func(context.Context, string, string) error { return nil })
	startFakeHostNetworkHelper(t, directory, func(hostnet.Desired) (bool, string) { return true, "" })
	if err := manager.applyHostNetwork(context.Background(), hopState(8443, 20000, 20010, true)); err != nil {
		t.Fatal(err)
	}
	desired, err := hostnet.ReadDesired(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.PortHops) != 1 || desired.PortHops[0] != (hostnet.PortHop{Start: 20000, End: 20010, Target: 8443}) {
		t.Fatalf("the helper did not receive the hop range: %+v", desired)
	}
	if err := manager.applyHostNetwork(context.Background(), desiredstate.State{}); err != nil {
		t.Fatal(err)
	}
	cleared, err := hostnet.ReadDesired(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.PortHops) != 0 {
		t.Fatalf("a revision without hops kept the hop table: %+v", cleared)
	}
}

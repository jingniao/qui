package desiredstate

import (
	"reflect"
	"testing"
)

func TestExtraHostExemptions(t *testing.T) {
	state := State{Network: &Network{HostSourceIPv4: "192.0.2.1", HostSourceIPv6: "2001:db8::1", OutboundSourceIP: "192.0.2.2"}}
	for i, ip := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.4", "192.0.2.3", "192.0.2.4", "::ffff:192.0.2.4", "2001:db8:0::4", "2001:db8::4", "2001:db8::3", "2001:db8:0::1", "invalid", ""} {
		state.Outbounds = append(state.Outbounds, Outbound{ID: string(rune('a' + i)), Enabled: true, SourceIP: ip})
	}
	state.Outbounds = append(state.Outbounds,
		Outbound{ID: "disabled", SourceIP: "192.0.2.5"},
		Outbound{ID: "disabled-v6", SourceIP: "2001:db8::5"},
	)
	v4, v6 := ExtraHostExemptions(state)
	if !reflect.DeepEqual(v4, []string{"192.0.2.3", "192.0.2.4"}) || !reflect.DeepEqual(v6, []string{"2001:db8::3", "2001:db8::4"}) {
		t.Fatalf("v4=%v v6=%v", v4, v6)
	}
	state.Network.HostSourceIPv4 = ""
	v4, v6 = ExtraHostExemptions(state)
	if len(v4) != 0 || !reflect.DeepEqual(v6, []string{"2001:db8::3", "2001:db8::4"}) {
		t.Fatalf("only IPv6 forced: v4=%v v6=%v", v4, v6)
	}
	state.Network.HostSourceIPv4 = "192.0.2.1"
	state.Network.HostSourceIPv6 = ""
	v4, v6 = ExtraHostExemptions(state)
	if !reflect.DeepEqual(v4, []string{"192.0.2.3", "192.0.2.4"}) || len(v6) != 0 {
		t.Fatalf("only IPv4 forced: v4=%v v6=%v", v4, v6)
	}
	state.Network = nil
	v4, v6 = ExtraHostExemptions(state)
	if len(v4)+len(v6) != 0 {
		t.Fatalf("no network: %v %v", v4, v6)
	}
}

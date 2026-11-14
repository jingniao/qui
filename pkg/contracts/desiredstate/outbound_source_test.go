package desiredstate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOutboundNetworkInheritance(t *testing.T) {
	server := Network{OutboundSourceIP: "192.0.2.1", OutboundInterface: "eth0", HostSourceIPv4: "192.0.2.2", HostSourceIPv6: "2001:db8::2"}
	state := State{
		Network: &server,
		Outbounds: []Outbound{
			{ID: "own", Enabled: true, SourceIP: "2001:db8::3", SourceInterface: "eth1"},
			{ID: "ip-only", Enabled: true, SourceIP: "192.0.2.3"},
			{ID: "interface-only", Enabled: true, SourceInterface: "eth2"},
			{ID: "inherited", Enabled: true},
		},
	}
	for _, tc := range []struct {
		id        string
		ip, iface string
	}{
		{id: "own", ip: "2001:db8::3", iface: "eth1"},
		{id: "ip-only", ip: "192.0.2.3", iface: "eth0"},
		{id: "interface-only", ip: "192.0.2.1", iface: "eth2"},
		{id: "inherited", ip: "192.0.2.1", iface: "eth0"},
		{id: "unknown", ip: "192.0.2.1", iface: "eth0"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			want := Network{OutboundSourceIP: tc.ip, OutboundInterface: tc.iface, HostSourceIPv4: "192.0.2.2", HostSourceIPv6: "2001:db8::2"}
			if got := state.OutboundNetwork(tc.id); got != want {
				t.Fatalf("OutboundNetwork(%q) = %+v, want %+v", tc.id, got, want)
			}
		})
	}
	if server.OutboundSourceIP != "192.0.2.1" || server.OutboundInterface != "eth0" {
		t.Fatalf("outbound lookup changed server settings: %+v", server)
	}
}

func TestOutboundNetworkWithoutServerSettings(t *testing.T) {
	state := State{Outbounds: []Outbound{
		{ID: "own", Enabled: true, SourceIP: "192.0.2.3", SourceInterface: "eth1"},
		{ID: "ip-only", Enabled: true, SourceIP: "2001:db8::3"},
		{ID: "interface-only", Enabled: true, SourceInterface: "eth2"},
		{ID: "unbound", Enabled: true},
	}}
	for _, tc := range []struct {
		id   string
		want Network
	}{
		{id: "own", want: Network{OutboundSourceIP: "192.0.2.3", OutboundInterface: "eth1"}},
		{id: "ip-only", want: Network{OutboundSourceIP: "2001:db8::3"}},
		{id: "interface-only", want: Network{OutboundInterface: "eth2"}},
		{id: "unbound", want: Network{}},
		{id: "unknown", want: Network{}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			if got := state.OutboundNetwork(tc.id); got != tc.want {
				t.Fatalf("OutboundNetwork(%q) = %+v, want %+v", tc.id, got, tc.want)
			}
		})
	}
}

func TestOutboundSourceValidation(t *testing.T) {
	for _, tc := range []struct {
		ip, iface    string
		block, valid bool
	}{
		{"192.0.2.1", "eth0", false, true},
		{"2001:db8::1", "eth0.100", false, true},
		{"", "wg0:1", false, true},
		{"bad", "", false, false},
		{"0.0.0.0", "", false, false},
		{"", "eth 0", false, false},
		{"192.0.2.1", "", true, false},
		{"", "eth0", true, false},
	} {
		s := valid()
		o := &s.Outbounds[0]
		o.SourceIP, o.SourceInterface = tc.ip, tc.iface
		if tc.block {
			o.Protocol = "block"
		}
		if err := s.Validate(); (err == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}

func TestOutboundSourceOmitted(t *testing.T) {
	b, err := json.Marshal(Outbound{ID: "direct", Protocol: "direct", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"id":"direct","protocol":"direct","enabled":true,"settings":{}}` || strings.Contains(string(b), "source_") {
		t.Fatalf("changed serialization: %s", b)
	}
}

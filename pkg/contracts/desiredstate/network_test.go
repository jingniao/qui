package desiredstate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNetworkValidationAcceptsOnlyUnicastHostAddressesAndInterfaces(t *testing.T) {
	cases := []struct {
		name    string
		network Network
		valid   bool
	}{
		{name: "unconfigured", network: Network{}, valid: true},
		{name: "public ipv4 source", network: Network{OutboundSourceIP: "192.0.2.10"}, valid: true},
		{name: "public ipv6 source", network: Network{OutboundSourceIP: "2001:db8::10"}, valid: true},
		{name: "private ipv4 source", network: Network{OutboundSourceIP: "10.20.30.40"}, valid: true},
		{name: "mapped ipv4 source", network: Network{OutboundSourceIP: "::ffff:192.0.2.10"}, valid: true},
		{name: "interface", network: Network{OutboundInterface: "eth0"}, valid: true},
		{name: "interface with vlan dot", network: Network{OutboundInterface: "eth0.100"}, valid: true},
		{name: "interface with colon", network: Network{OutboundInterface: "wg0:1"}, valid: true},
		{name: "interface at maximum length", network: Network{OutboundInterface: strings.Repeat("a", 15)}, valid: true},
		{name: "both host families", network: Network{HostSourceIPv4: "198.51.100.4", HostSourceIPv6: "2001:db8::4"}, valid: true},
		{name: "unparsable source", network: Network{OutboundSourceIP: "not-an-address"}, valid: false},
		{name: "source with zone", network: Network{OutboundSourceIP: "fe80::1%eth0"}, valid: false},
		{name: "unspecified ipv4 source", network: Network{OutboundSourceIP: "0.0.0.0"}, valid: false},
		{name: "unspecified ipv6 source", network: Network{OutboundSourceIP: "::"}, valid: false},
		{name: "loopback ipv4 source", network: Network{OutboundSourceIP: "127.0.0.1"}, valid: false},
		{name: "loopback ipv6 source", network: Network{OutboundSourceIP: "::1"}, valid: false},
		{name: "multicast source", network: Network{OutboundSourceIP: "239.1.2.3"}, valid: false},
		{name: "broadcast source", network: Network{OutboundSourceIP: "255.255.255.255"}, valid: false},
		{name: "link local source", network: Network{OutboundSourceIP: "169.254.10.1"}, valid: false},
		{name: "link local ipv6 source", network: Network{OutboundSourceIP: "fe80::1"}, valid: false},
		{name: "ipv6 in the ipv4 field", network: Network{HostSourceIPv4: "2001:db8::4"}, valid: false},
		{name: "ipv4 in the ipv6 field", network: Network{HostSourceIPv6: "198.51.100.4"}, valid: false},
		{name: "mapped ipv4 in the ipv6 field", network: Network{HostSourceIPv6: "::ffff:198.51.100.4"}, valid: false},
		{name: "loopback host ipv4", network: Network{HostSourceIPv4: "127.0.0.1"}, valid: false},
		{name: "unparsable host ipv6", network: Network{HostSourceIPv6: "not-an-address"}, valid: false},
		{name: "interface with space", network: Network{OutboundInterface: "eth 0"}, valid: false},
		{name: "interface with slash", network: Network{OutboundInterface: "eth0/1"}, valid: false},
		{name: "interface with newline", network: Network{OutboundInterface: "eth0\n"}, valid: false},
		{name: "interface too long", network: Network{OutboundInterface: strings.Repeat("a", 16)}, valid: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := test.network.Validate()
			if test.valid && err != nil {
				t.Fatalf("valid settings rejected: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid settings accepted")
			}
			state := valid()
			state.Network = &test.network
			if (state.Validate() == nil) != test.valid {
				t.Fatalf("state validation disagrees with the settings validator: %+v", test.network)
			}
		})
	}
}

func TestNetworkIsOmittedUnlessConfigured(t *testing.T) {
	state := valid()
	if state.Network != nil {
		t.Fatal("network block appeared on an untouched state")
	}
	if NetworkOrNil("", "", "", "") != nil {
		t.Fatal("empty settings produced a network block")
	}
	if network := NetworkOrNil("", "eth0", "", ""); network == nil || network.OutboundInterface != "eth0" {
		t.Fatal("configured interface was dropped")
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "network") {
		t.Fatalf("unused settings serialized: %s", data)
	}
	// An empty block written by an older or hand-built state is normalized away
	// so the bytes stay identical to a state that never carried the field.
	withEmptyBlock := strings.Replace(string(data), `"logging":{"domain_only":true}`, `"logging":{"domain_only":true},"network":{}`, 1)
	decoded, err := Decode([]byte(withEmptyBlock))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Network != nil {
		t.Fatal("empty network block survived decoding")
	}
	roundTrip, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(roundTrip) != string(data) {
		t.Fatalf("round trip changed the state bytes:\n got %s\nwant %s", roundTrip, data)
	}
	// A populated block is decoded and validated like any other state field.
	populated := strings.Replace(string(data), `"logging":{"domain_only":true}`, `"logging":{"domain_only":true},"network":{"outbound_source_ip":"192.0.2.10","outbound_interface":"eth0","host_source_ipv4":"198.51.100.4","host_source_ipv6":"2001:db8::4"}`, 1)
	decoded, err = Decode([]byte(populated))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Network == nil || decoded.Network.OutboundSourceIP != "192.0.2.10" || decoded.Network.OutboundInterface != "eth0" || decoded.Network.HostSourceIPv4 != "198.51.100.4" || decoded.Network.HostSourceIPv6 != "2001:db8::4" {
		t.Fatalf("populated network block lost: %+v", decoded.Network)
	}
	invalid := strings.Replace(string(data), `"logging":{"domain_only":true}`, `"logging":{"domain_only":true},"network":{"host_source_ipv4":"2001:db8::4"}`, 1)
	if _, err := Decode([]byte(invalid)); err == nil {
		t.Fatal("invalid network block accepted")
	}
}

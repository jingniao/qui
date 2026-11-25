package hostnet

import (
	"fmt"
	"strings"
	"testing"
)

// TestRenderPanelExempt: connections to the panel keep the host's own source, so
// the translation rule of each family carries the panel destinations.
func TestRenderPanelExempt(t *testing.T) {
	d := Desired{ID: "panel", IPv4: "198.51.100.4", IPv6: "2001:db8::4",
		PanelIPv4: []string{"203.0.113.5", "::ffff:203.0.113.5", "203.0.113.6"},
		PanelIPv6: []string{"2001:db8:1::5", "2001:db8:1:0::5"}}
	rules, err := Render(d, testInterfaces)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`ip saddr != { 198.51.100.4 } ip daddr != { 203.0.113.5, 203.0.113.6 } snat ip to 198.51.100.4`,
		`ip6 saddr != { 2001:db8::4 } ip6 daddr != { 2001:db8:1::5 } snat ip6 to 2001:db8::4`,
	} {
		if !strings.Contains(string(rules), want) {
			t.Fatalf("missing %q in %s", want, rules)
		}
	}
	d.PanelIPv4, d.PanelIPv6 = nil, nil
	rules, err = Render(d, testInterfaces)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rules), "daddr") {
		t.Fatalf("no panel addresses still rendered a destination match: %s", rules)
	}
}

func TestValidatePanelAddresses(t *testing.T) {
	many := make([]string, MaxPanelAddresses+1)
	for i := range many {
		many[i] = fmt.Sprintf("203.0.113.%d", i+1)
	}
	for _, tc := range []struct {
		name       string
		ipv4, ipv6 []string
		ok         bool
	}{
		{name: "none", ok: true},
		{name: "both families", ipv4: []string{"203.0.113.5"}, ipv6: []string{"2001:db8::5"}, ok: true},
		{name: "loopback panel", ipv4: []string{"127.0.0.1"}, ok: true},
		{name: "IPv6 in the IPv4 list", ipv4: []string{"2001:db8::5"}},
		{name: "IPv4 in the IPv6 list", ipv6: []string{"203.0.113.5"}},
		{name: "mapped IPv4 in the IPv6 list", ipv6: []string{"::ffff:203.0.113.5"}},
		{name: "not an address", ipv4: []string{"panel.example"}},
		{name: "unspecified", ipv4: []string{"0.0.0.0"}},
		{name: "multicast", ipv6: []string{"ff02::1"}},
		{name: "zone", ipv6: []string{"fe80::1%eth0"}},
		{name: "too many", ipv4: many},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Desired{ID: "x", IPv4: "198.51.100.4", PanelIPv4: tc.ipv4, PanelIPv6: tc.ipv6}.Validate()
			if (err == nil) != tc.ok {
				t.Fatalf("validate = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

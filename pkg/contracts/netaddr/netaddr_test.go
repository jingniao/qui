package netaddr

import (
	"net"
	"testing"
)

func interfaceAddresses(values ...string) []net.Addr {
	addrs := make([]net.Addr, 0, len(values))
	for _, value := range values {
		ip := net.ParseIP(value)
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		addrs = append(addrs, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return addrs
}

// A public IPv4 wins even when the interface lists an IPv6 first, because the
// IPv4 is what most clients report and diagnose.
func TestSelectPrefersPublicIPv4OverIPv6(t *testing.T) {
	addrs := interfaceAddresses("2001:db8::1", "fe80::1", "10.0.0.5", "203.0.113.7")
	if got := Select(addrs); got != "203.0.113.7" {
		t.Fatalf("select: %q", got)
	}
}

func TestSelectFallsBackToPublicIPv6(t *testing.T) {
	if got := Select(interfaceAddresses("2001:db8::1", "2001:db8::2")); got != "2001:db8::1" {
		t.Fatalf("select: %q", got)
	}
}

// Nothing dialable means nothing reported: the heartbeat stays empty instead of
// advertising an address no client can reach.
func TestSelectReportsNothingWhenEveryAddressIsPrivate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address []string
	}{
		{name: "rfc1918 and rfc4193", address: []string{"10.0.0.5", "192.168.1.5", "172.16.0.1", "fd00::1"}},
		{name: "cgnat", address: []string{"100.64.0.1", "100.127.255.254"}},
		{name: "link-local", address: []string{"169.254.10.20", "fe80::1"}},
		{name: "loopback and unspecified", address: []string{"127.0.0.1", "::1", "0.0.0.0", "::"}},
		{name: "multicast", address: []string{"224.0.0.1", "ff02::1"}},
		{name: "empty", address: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Select(interfaceAddresses(tc.address...)); got != "" {
				t.Fatalf("select: %q", got)
			}
		})
	}
}

func TestPublicJudgesOneAddress(t *testing.T) {
	for _, value := range []string{"203.0.113.7", "2001:db8::1"} {
		if !Public(net.ParseIP(value)) {
			t.Fatalf("%s refused", value)
		}
	}
	for _, value := range []string{"100.64.0.1", "fd00::1", "169.254.1.1", "127.0.0.1"} {
		if Public(net.ParseIP(value)) {
			t.Fatalf("%s accepted", value)
		}
	}
	if Public(nil) {
		t.Fatal("a missing address was accepted")
	}
}

// ValidHostName is the install --panel rule: dotted labels only, no scheme,
// port, brackets or whitespace.
func TestValidHostNameAcceptsNamesAndRefusesDialSyntax(t *testing.T) {
	for _, value := range []string{"panel.example.com", "203.0.113.10", "edge-1.example.com", "PANEL.Example.COM"} {
		if !ValidHostName(value) {
			t.Fatalf("%q refused", value)
		}
	}
	for _, value := range []string{"", "http://x", "a:80", "[::1]", "2001:db8::1", "panel example.com", ".panel.example.com", "panel.example.com.", "panel..example.com", "-panel.example.com", "panel-.example.com"} {
		if ValidHostName(value) {
			t.Fatalf("%q accepted", value)
		}
	}
}

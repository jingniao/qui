package capability

import (
	"fmt"
	"net"
	"testing"
)

func parseCIDR(s string) net.Addr {
	ip, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	ipNet.IP = ip
	return ipNet
}

func TestPublicAddresses(t *testing.T) {
	ifaces := []interfaceAddrs{
		{
			Name:     "lo",
			Up:       true,
			Loopback: true,
			Addrs:    []net.Addr{parseCIDR("127.0.0.1/8"), parseCIDR("::1/128")},
		},
		{
			Name:     "eth1",
			Up:       true,
			Loopback: false,
			Addrs:    []net.Addr{parseCIDR("198.51.100.7/24")},
		},
		{
			Name:     "eth0",
			Up:       true,
			Loopback: false,
			Addrs: []net.Addr{
				parseCIDR("2001:db8::5/64"), // IPv6
				parseCIDR("10.0.0.2/24"),    // Private
				parseCIDR("203.0.113.5/24"), // IPv4
			},
		},
		{
			Name:     "dummy0",
			Up:       false,
			Loopback: false,
			Addrs:    []net.Addr{parseCIDR("203.0.113.99/24")},
		},
	}

	result := publicAddresses(ifaces, "eth0")

	// Expect:
	// eth0 default route first: IPv4 (203.0.113.5), then IPv6 (2001:db8::5)
	// then eth1: 198.51.100.7
	// down and loopback ignored
	if len(result) != 3 {
		t.Fatalf("expected 3 addresses, got %d: %+v", len(result), result)
	}
	expected := []struct {
		addr  string
		iface string
	}{
		{"203.0.113.5", "eth0"},
		{"2001:db8::5", "eth0"},
		{"198.51.100.7", "eth1"},
	}
	for i, exp := range expected {
		if result[i].Address != exp.addr || result[i].Interface != exp.iface {
			t.Errorf("at index %d: expected %s/%s, got %s/%s", i, exp.addr, exp.iface, result[i].Address, result[i].Interface)
		}
	}

	// Test 20 addresses capped to 16
	var manyAddrs []net.Addr
	for i := 1; i <= 20; i++ {
		manyAddrs = append(manyAddrs, parseCIDR(fmt.Sprintf("203.0.113.%d/32", i)))
	}
	manyIfaces := []interfaceAddrs{
		{
			Name:     "eth0",
			Up:       true,
			Loopback: false,
			Addrs:    manyAddrs,
		},
	}
	capped := publicAddresses(manyIfaces, "eth0")
	if len(capped) != 16 {
		t.Fatalf("expected 16 capped addresses, got %d", len(capped))
	}
}

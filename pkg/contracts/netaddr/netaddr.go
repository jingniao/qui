// Package netaddr holds the two address rules the panel and the Agent must
// agree on: which address a client on the Internet can dial, and which host text
// may be stored as one. Keeping them here means the panel never classifies an
// address differently from the Agent that reported it.
package netaddr

import (
	"net"
	"regexp"
	"strings"
)

// cgnat is 100.64.0.0/10. net.IP.IsPrivate covers RFC 1918 and RFC 4193 only,
// and a carrier-grade NAT address is neither private nor dialable from outside.
var cgnat = net.IPNet{IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32)}

// hostPattern is the rule deploy/linux/node-install.sh applies to --panel.
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

// Public reports whether ip is a global unicast address that a client elsewhere
// can reach. Loopback, link-local, private, CGNAT, unspecified and multicast
// addresses are not, so a host behind NAT never advertises one of them.
func Public(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsPrivate() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		return !cgnat.Contains(v4)
	}
	return true
}

// Select returns the address to advertise from a candidate list: the first
// public IPv4, otherwise the first public IPv6. Empty when nothing qualifies.
func Select(addrs []net.Addr) string {
	var fallback string
	for _, addr := range addrs {
		ip := addressIP(addr)
		if !Public(ip) {
			continue
		}
		if ip.To4() != nil {
			return ip.String()
		}
		if fallback == "" {
			fallback = ip.String()
		}
	}
	return fallback
}

// addressIP reads the IP out of the address types an interface list and a gRPC
// peer produce. Anything else carries no address this package can judge.
func addressIP(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	case *net.TCPAddr:
		return value.IP
	case *net.UDPAddr:
		return value.IP
	}
	return nil
}

// ValidHostName reports whether value is a DNS name or a dotted number that can
// be stored and dialled verbatim: no scheme, port, brackets or whitespace, and
// every dot-separated label non-empty, at most 63 bytes and not starting or
// ending with a hyphen. It says nothing about IP literals; callers that accept
// them check net.ParseIP first.
func ValidHostName(value string) bool {
	if value == "" || len(value) > 253 || !hostPattern.MatchString(value) {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

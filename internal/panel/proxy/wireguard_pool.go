package proxy

import (
	"errors"
	"net/netip"
	"proxycontrol/pkg/contracts/desiredstate"
)

// The first interface prefix in each address family defines its managed pool.
// All interface addresses and explicit peer routes remain reserved.
func wireguardPools(settings desiredstate.ProtocolSettings) ([]netip.Prefix, []netip.Prefix, error) {
	var pools []netip.Prefix
	var reserved []netip.Prefix
	if settings.WireGuard == nil || len(settings.WireGuard.Addresses) == 0 {
		return nil, nil, errors.New("WireGuard address pool required")
	}
	for _, value := range settings.WireGuard.Addresses {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			address, parseErr := netip.ParseAddr(value)
			if parseErr != nil {
				return nil, nil, errors.New("invalid WireGuard interface address")
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		address := prefix.Addr()
		if address.Is4In6() || address.IsUnspecified() || address.IsMulticast() || address.IsLoopback() {
			return nil, nil, errors.New("WireGuard managed pool requires unicast IPv4 or IPv6 addresses")
		}
		found := false
		for _, pool := range pools {
			if pool.Addr().Is4() == address.Is4() {
				found = true
			}
		}
		if !found {
			pools = append(pools, prefix.Masked())
		}
		reserved = append(reserved, netip.PrefixFrom(address, address.BitLen()))
	}
	for _, peer := range settings.WireGuard.Peers {
		for _, value := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(value)
			if err != nil || prefix.Addr().Is4In6() {
				return nil, nil, errors.New("invalid WireGuard reserved peer network")
			}
			reserved = append(reserved, prefix.Masked())
		}
	}
	return pools, reserved, nil
}

func lastWireguardAddress(prefix netip.Prefix) netip.Addr {
	address := prefix.Masked().Addr()
	if address.Is4() {
		bytes := address.As4()
		for bit := prefix.Bits(); bit < 32; bit++ {
			bytes[bit/8] |= 1 << uint(7-bit%8)
		}
		return netip.AddrFrom4(bytes)
	}
	bytes := address.As16()
	for bit := prefix.Bits(); bit < 128; bit++ {
		bytes[bit/8] |= 1 << uint(7-bit%8)
	}
	return netip.AddrFrom16(bytes)
}

func validateWireguardAssignments(pools []netip.Prefix, reserved, assigned []netip.Prefix) error {
	for _, prefix := range assigned {
		address := prefix.Addr()
		valid := false
		for _, pool := range pools {
			if pool.Contains(address) && !((address.Is6() || pool.Bits() < 31) && address == pool.Addr()) && !(address.Is4() && pool.Bits() < 31 && address == lastWireguardAddress(pool)) {
				valid = true
			}
		}
		if !valid {
			return errors.New("WireGuard pool change would invalidate an existing device or relay address")
		}
		for _, network := range reserved {
			if network.Contains(address) {
				return errors.New("WireGuard interface or manual peer conflicts with an assigned device or relay address")
			}
		}
	}
	return nil
}

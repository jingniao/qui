package desiredstate

import (
	"net"
	"slices"
)

// ExtraHostExemptions returns canonical source addresses requiring host exemptions.
func ExtraHostExemptions(state State) (v4, v6 []string) {
	if state.Network == nil {
		return
	}
	network := state.Network
	for _, family := range []int{4, 6} {
		forced := network.HostSourceIPv4
		if family == 6 {
			forced = network.HostSourceIPv6
		}
		if forced == "" {
			continue
		}
		addresses := map[string]bool{}
		for _, outbound := range state.Outbounds {
			if !outbound.Enabled {
				continue
			}
			ip := net.ParseIP(state.OutboundNetwork(outbound.ID).OutboundSourceIP)
			if ip == nil || (ip.To4() != nil) != (family == 4) || ip.Equal(net.ParseIP(network.OutboundSourceIP)) || ip.Equal(net.ParseIP(forced)) {
				continue
			}
			addresses[ip.String()] = true
		}
		var extra []string
		for address := range addresses {
			extra = append(extra, address)
		}
		slices.Sort(extra)
		if family == 4 {
			v4 = extra
		} else {
			v6 = extra
		}
	}
	return
}

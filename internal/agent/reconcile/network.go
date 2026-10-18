package reconcile

import (
	"errors"
	"fmt"
	"net"
	"proxycontrol/pkg/contracts/desiredstate"
)

// networkUnavailableCode is the temporary apply failure code of a node whose
// configured outbound source address or interface is not present locally. It is
// never cached: the address may appear later (a lease, a hotplugged link, a
// tunnel that comes up after boot), so the revision is retried.
const networkUnavailableCode = "network_unavailable"

// networkAvailable reports whether the node's outbound source address and
// interface exist locally. It is injectable so tests can simulate an address
// that appears later; the default inspects the host.
type networkAvailable func(desiredstate.Network) error

// verifyNetworkAvailable is the host check used unless a Reconciler injects its
// own: the source address must be assigned to a local interface, the interface
// must exist, and when both are set the address must be on that interface.
func verifyNetworkAvailable(network desiredstate.Network) error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	addresses := map[string][]net.IP{}
	names := map[string]bool{}
	for _, entry := range interfaces {
		names[entry.Name] = true
		assigned, err := entry.Addrs()
		if err != nil {
			return err
		}
		for _, address := range assigned {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil {
				return err
			}
			addresses[entry.Name] = append(addresses[entry.Name], ip)
		}
	}
	return checkNetworkAvailable(network, names, addresses)
}

// checkNetworkAvailable is the pure part of the check, shared with tests.
func checkNetworkAvailable(network desiredstate.Network, names map[string]bool, addresses map[string][]net.IP) error {
	if network.OutboundInterface != "" && !names[network.OutboundInterface] {
		return fmt.Errorf("outbound interface %s does not exist on this node", network.OutboundInterface)
	}
	if network.OutboundSourceIP == "" {
		return nil
	}
	source := net.ParseIP(network.OutboundSourceIP)
	if source == nil {
		return errors.New("outbound source IP does not parse")
	}
	interfaces := []string{}
	if network.OutboundInterface != "" {
		interfaces = append(interfaces, network.OutboundInterface)
	} else {
		for name := range names {
			interfaces = append(interfaces, name)
		}
	}
	for _, name := range interfaces {
		for _, candidate := range addresses[name] {
			if candidate.Equal(source) {
				return nil
			}
		}
	}
	if network.OutboundInterface != "" {
		return fmt.Errorf("outbound source IP %s is not assigned to %s", network.OutboundSourceIP, network.OutboundInterface)
	}
	return fmt.Errorf("outbound source IP %s is not assigned to any local interface", network.OutboundSourceIP)
}

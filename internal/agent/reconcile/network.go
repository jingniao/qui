package reconcile

import (
	"errors"
	"fmt"
	"net"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// networkUnavailableCode is the temporary apply failure code of a node whose
// configured outbound source address or interface is not present locally. It is
// never cached: the address may appear later (a lease, a hotplugged link, a
// tunnel that comes up after boot), so the revision is retried.
const networkUnavailableCode = "network_unavailable"

// Fixed codes for failures before the core is touched: the storage format
// preflight, and writing the revision's sealed snapshots. Neither is cached.
const (
	storagePreflightFailedCode = "storage_preflight_failed"
	revisionStoreFailedCode    = "revision_store_failed"
)

// networkAvailable reports whether the node's outbound source address and
// interface exist locally. It is injectable so tests can simulate an address
// that appears later; the default inspects the host.
type networkAvailable func(desiredstate.Network) error

// verifyOutboundNetworks checks the same effective bindings the native cores
// render, before any runtime or retained revision is changed. The node binding
// also applies to core-owned internal outbounds, so it is checked separately.
func (r *Reconciler) verifyOutboundNetworks(state desiredstate.State) error {
	available := r.NetworkAvailable
	if available == nil {
		available = verifyNetworkAvailable
	}
	if state.Network != nil {
		if err := available(*state.Network); err != nil {
			return err
		}
	}
	outbounds := state.Outbounds
	if _, multi := r.Adapter.(*core.MultiRuntime); multi {
		// MultiRuntime renders only active cores and their reachable graphs.
		// GOST does not render these source bindings or user outbounds.
		outbounds = nil
		seen := map[string]bool{}
		for _, inbound := range state.Inbounds {
			name := inbound.Core
			if name == "" {
				name = r.Adapter.Name()
			}
			if !inbound.Enabled || name == desiredstate.CoreGost || seen[name] {
				continue
			}
			seen[name] = true
			outbounds = append(outbounds, state.ForCore(name, r.Adapter.Name()).Outbounds...)
		}
	}
	checked := map[desiredstate.Network]bool{}
	for _, outbound := range outbounds {
		if !outbound.Enabled || outbound.Protocol == "block" || outbound.SourceIP == "" && outbound.SourceInterface == "" {
			continue
		}
		network := state.OutboundNetwork(outbound.ID)
		if checked[network] {
			continue
		}
		if err := available(network); err != nil {
			return fmt.Errorf("outbound %s: %w", outbound.ID, err)
		}
		checked[network] = true
	}
	return nil
}

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

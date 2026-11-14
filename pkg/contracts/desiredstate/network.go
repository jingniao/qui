package desiredstate

import (
	"errors"
	"net"
	"regexp"
)

// Network carries the outbound network settings of one node: the source address
// and interface the proxy core dials from, and the source addresses the whole
// server is forced to use. State.Network is nil for nodes that configure none
// of them, so their serialized state stays byte-identical to the states written
// before this feature existed.
type Network struct {
	OutboundSourceIP  string `json:"outbound_source_ip,omitempty"`
	OutboundInterface string `json:"outbound_interface,omitempty"`
	HostSourceIPv4    string `json:"host_source_ipv4,omitempty"`
	HostSourceIPv6    string `json:"host_source_ipv6,omitempty"`
}

// interfaceNamePattern is the Linux interface name grammar (IFNAMSIZ-1 bytes).
var interfaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)

// Empty reports whether nothing is configured. An empty Network serializes as
// if the state carried no network field at all.
func (n Network) Empty() bool {
	return n.OutboundSourceIP == "" && n.OutboundInterface == "" && n.HostSourceIPv4 == "" && n.HostSourceIPv6 == ""
}

// NetworkOrNil returns the settings only when at least one value is configured.
// Every state builder uses it, so a node that does not use the feature keeps
// the previous serialized bytes and an older Agent never sees the field.
func NetworkOrNil(outboundSourceIP, outboundInterface, hostSourceIPv4, hostSourceIPv6 string) *Network {
	network := Network{OutboundSourceIP: outboundSourceIP, OutboundInterface: outboundInterface, HostSourceIPv4: hostSourceIPv4, HostSourceIPv6: hostSourceIPv6}
	if network.Empty() {
		return nil
	}
	return &network
}

// OutboundNetwork is the source binding one outbound dials from (v1.5.5 item
// 17): its own source IP and interface, each falling back to the node's core
// outbound settings. An unknown ID (an internal outbound) gets the node's.
func (s State) OutboundNetwork(id string) Network {
	var network Network
	if s.Network != nil {
		network = *s.Network
	}
	for _, outbound := range s.Outbounds {
		if outbound.ID != id {
			continue
		}
		if outbound.SourceIP != "" {
			network.OutboundSourceIP = outbound.SourceIP
		}
		if outbound.SourceInterface != "" {
			network.OutboundInterface = outbound.SourceInterface
		}
		break
	}
	return network
}

// Baseline is the state every published configuration starts from: no users,
// inbounds or policies, only the built-in direct and block outbounds and the
// node's outbound network. Published as is (v1.5.5 item 11) it gives a node a
// first confirmed revision without starting any proxy core.
func Baseline(nodeID string, network *Network) State {
	return State{SchemaVersion: 1, Revision: 1, NodeID: nodeID, Logging: Logging{DomainOnly: true}, Users: []User{}, Credentials: []Credential{}, Inbounds: []Inbound{}, Outbounds: []Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}, OutboundGroups: []OutboundGroup{}, ProxyChains: []ProxyChain{}, Policies: []Policy{}, Network: network}
}

// Validate checks the permanent syntax of the settings. It never resolves an
// address or an interface: whether they exist is decided on the node when the
// configuration is applied.
func (n Network) Validate() error {
	if n.OutboundSourceIP != "" {
		if err := validateSourceIP(n.OutboundSourceIP, 0); err != nil {
			return errors.New("invalid outbound source IP")
		}
	}
	if n.HostSourceIPv4 != "" {
		if err := validateSourceIP(n.HostSourceIPv4, 4); err != nil {
			return errors.New("invalid host source IPv4")
		}
	}
	if n.HostSourceIPv6 != "" {
		if err := validateSourceIP(n.HostSourceIPv6, 6); err != nil {
			return errors.New("invalid host source IPv6")
		}
	}
	if n.OutboundInterface != "" && !interfaceNamePattern.MatchString(n.OutboundInterface) {
		return errors.New("invalid outbound interface")
	}
	return nil
}

// validateSourceIP accepts a unicast host address of the requested family
// (0 means any family).
func validateSourceIP(value string, family int) error {
	ip := net.ParseIP(value)
	if ip == nil {
		return errors.New("address does not parse")
	}
	if family == 4 && ip.To4() == nil {
		return errors.New("address is not IPv4")
	}
	if family == 6 && ip.To4() != nil {
		return errors.New("address is not IPv6")
	}
	// IsGlobalUnicast rejects the unspecified, loopback, multicast, link-local
	// and IPv4 broadcast addresses.
	if !ip.IsGlobalUnicast() {
		return errors.New("address is not a unicast host address")
	}
	return nil
}

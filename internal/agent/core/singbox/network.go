package singbox

import (
	"errors"
	"net/netip"
	"proxycontrol/pkg/contracts/desiredstate"
)

// domainResolver is the dial-field resolver of one outbound. It reuses the
// configuration's own resolver tag and only narrows the family, so a name still
// resolves through the node's system DNS.
type domainResolver struct {
	Server   string `json:"server"`
	Strategy string `json:"strategy,omitempty"`
}

// applyOutboundSource pins the dialing outbounds and the outbound WireGuard
// endpoints using their own values, inheriting each unset field from the node's
// source address and interface: the family bind address, the interface, and a
// resolver strategy that keeps names resolving to the family of the source
// address. Block outbounds never dial and inbound WireGuard endpoints only
// listen, so both are left untouched. The step runs last, so group members and
// WireGuard endpoints are covered too.
//
// Known limitation: a target that is an IP literal of the other family (or a
// loopback address) cannot be reached from the pinned source address and fails
// to connect.
func applyOutboundSource(doc *document, state desiredstate.State) error {
	// Outbound tags are the desired outbound IDs (group members are referenced,
	// not copied), so each outbound looks up its own binding by tag.
	dial := func(outbound *outbound) error {
		network := state.OutboundNetwork(outbound.Tag)
		bind, strategy, err := sourceBind(network.OutboundSourceIP)
		if err != nil {
			return err
		}
		outbound.BindInterface = network.OutboundInterface
		if bind == "inet4_bind_address" {
			outbound.Inet4BindAddress = network.OutboundSourceIP
			outbound.Inet6BindAddress = ""
		} else if bind == "inet6_bind_address" {
			outbound.Inet6BindAddress = network.OutboundSourceIP
			outbound.Inet4BindAddress = ""
		}
		if strategy != "" {
			outbound.DomainResolver = &domainResolver{Server: "_panel_dns_", Strategy: strategy}
		}
		return nil
	}
	for index := range doc.Outbounds {
		if !dialable(doc.Outbounds[index].Type) {
			continue
		}
		if err := dial(&doc.Outbounds[index]); err != nil {
			return err
		}
	}
	// Only outbound WireGuard endpoints dial; an inbound endpoint is a listener.
	outboundEndpoints := map[string]bool{}
	for _, entry := range state.Outbounds {
		if entry.Enabled && entry.Protocol == "wireguard" {
			outboundEndpoints[entry.ID] = true
		}
	}
	for index := range doc.Endpoints {
		if !outboundEndpoints[doc.Endpoints[index].Tag] {
			continue
		}
		endpoint := &doc.Endpoints[index]
		network := state.OutboundNetwork(endpoint.Tag)
		bind, strategy, err := sourceBind(network.OutboundSourceIP)
		if err != nil {
			return err
		}
		endpoint.BindInterface = network.OutboundInterface
		if bind == "inet4_bind_address" {
			endpoint.Inet4BindAddress = network.OutboundSourceIP
			endpoint.Inet6BindAddress = ""
		} else if bind == "inet6_bind_address" {
			endpoint.Inet6BindAddress = network.OutboundSourceIP
			endpoint.Inet4BindAddress = ""
		}
		if strategy != "" {
			endpoint.DomainResolver = &domainResolver{Server: "_panel_dns_", Strategy: strategy}
		}
	}
	return nil
}

// dialable reports whether an outbound type accepts dial fields. Block never
// dials, and the selector and urltest groups only choose members: sing-box
// rejects dial fields on all three.
func dialable(kind string) bool {
	switch kind {
	case "block", "selector", "urltest":
		return false
	}
	return true
}

// sourceBind returns the dial-field name that takes the configured source
// address and the resolver strategy that pins names to its family.
func sourceBind(source string) (string, string, error) {
	if source == "" {
		return "", "", nil
	}
	address, err := netip.ParseAddr(source)
	if err != nil {
		return "", "", errors.New("outbound source IP does not parse")
	}
	if address.Unmap().Is4() {
		return "inet4_bind_address", "ipv4_only", nil
	}
	return "inet6_bind_address", "ipv6_only", nil
}

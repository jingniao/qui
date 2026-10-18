package xray

import (
	"errors"
	"net"
	"net/netip"
	"proxycontrol/pkg/contracts/desiredstate"
)

// applyOutboundSource pins every dialing outbound of a rendered configuration to
// the node's configured outbound source address and interface: sendThrough on
// the outbound, sockopt.interface for the socket, and a family strategy that
// keeps a name resolving to the family of the source address. Blackhole
// outbounds never dial and are left untouched. The check runs after the final
// outbound list exists, so balancer members and probe targets are covered too.
//
// Known limitation: an outbound target that is an IP literal of the other
// family (or a loopback address) cannot be reached from the pinned source
// address and fails to connect.
func applyOutboundSource(document *configDocument, network *desiredstate.Network) error {
	if network == nil || network.OutboundSourceIP == "" && network.OutboundInterface == "" {
		return nil
	}
	source, err := sourceAddress(network.OutboundSourceIP)
	if err != nil {
		return err
	}
	strategy := familyStrategy(source)
	for index := range document.Outbounds {
		outbound := &document.Outbounds[index]
		if outbound.Protocol == "blackhole" {
			continue
		}
		outbound.SendThrough = source
		if network.OutboundInterface != "" {
			sockopt(outbound).Interface = network.OutboundInterface
		}
		if strategy == "" {
			continue
		}
		// Freedom resolves the target itself, and its settings carry the
		// strategy; every other protocol takes it from the socket options.
		if outbound.Protocol == "freedom" {
			settings, err := freedomSettings(outbound.Settings)
			if err != nil {
				return err
			}
			settings["domainStrategy"] = strategy
			outbound.Settings = settings
			continue
		}
		sockopt(outbound).DomainStrategy = strategy
	}
	return nil
}

// sockopt returns the socket options of an outbound, creating the stream
// settings and the options when the outbound has none.
func sockopt(outbound *outboundConfig) *sockoptSettings {
	if outbound.Stream == nil {
		outbound.Stream = &streamSettings{Network: "tcp", Security: "none"}
	}
	if outbound.Stream.Sockopt == nil {
		outbound.Stream.Sockopt = &sockoptSettings{}
	}
	return outbound.Stream.Sockopt
}

// sourceAddress renders the configured source address canonically, so a mapped
// IPv4 address and its plain form produce the same configuration.
func sourceAddress(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return "", errors.New("outbound source IP does not parse")
	}
	return address.Unmap().String(), nil
}

// familyStrategy returns the Xray domain strategy that pins name resolution to
// the family of the source address.
func familyStrategy(source string) string {
	if source == "" {
		return ""
	}
	if address := net.ParseIP(source); address != nil && address.To4() == nil {
		return "ForceIPv6"
	}
	return "ForceIPv4"
}

// freedomSettings returns the freedom settings map of an outbound, creating it
// when the outbound has none.
func freedomSettings(settings any) (map[string]any, error) {
	switch value := settings.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		copied := make(map[string]any, len(value)+1)
		for key, entry := range value {
			copied[key] = entry
		}
		return copied, nil
	}
	return nil, errors.New("freedom outbound settings cannot carry a domain strategy")
}

package node

import (
	"context"
	"net"
	"time"

	"google.golang.org/grpc/peer"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/netaddr"
)

type PublicAddress struct {
	Address  string `json:"address"`
	Source   string `json:"source"`   // "interface:<name>" | "peer"
	Family   string `json:"family"`   // "ipv4" | "ipv6"
	LastSeen string `json:"lastSeen"` // RFC3339 UTC
}

// mergeCandidates replaces the reported set (reported addresses plus the peer, if public),
// keeping LastSeen of addresses seen before; addresses gone from the report are dropped.
func mergeCandidates(previous []PublicAddress, reported []*pb.PublicAddress, peer string, now time.Time) []PublicAddress {
	prevMap := make(map[string]PublicAddress, len(previous))
	for _, p := range previous {
		prevMap[p.Address] = p
	}

	seen := make(map[string]bool)
	var candidates []PublicAddress

	for _, r := range reported {
		ip := net.ParseIP(r.Address)
		if !netaddr.Public(ip) {
			continue
		}
		addrStr := ip.String()
		if seen[addrStr] {
			continue
		}
		seen[addrStr] = true

		fam := "ipv6"
		if ip.To4() != nil {
			fam = "ipv4"
		}
		src := "interface:" + r.Interface
		lastSeen := now.UTC().Format(time.RFC3339)
		if old, ok := prevMap[addrStr]; ok && old.LastSeen != "" {
			lastSeen = old.LastSeen
		}
		candidates = append(candidates, PublicAddress{
			Address:  addrStr,
			Source:   src,
			Family:   fam,
			LastSeen: lastSeen,
		})
		if len(candidates) >= 16 {
			return candidates
		}
	}

	if peer != "" {
		ip := net.ParseIP(peer)
		if netaddr.Public(ip) {
			addrStr := ip.String()
			if !seen[addrStr] {
				seen[addrStr] = true
				fam := "ipv6"
				if ip.To4() != nil {
					fam = "ipv4"
				}
				lastSeen := now.UTC().Format(time.RFC3339)
				if old, ok := prevMap[addrStr]; ok && old.LastSeen != "" {
					lastSeen = old.LastSeen
				}
				candidates = append(candidates, PublicAddress{
					Address:  addrStr,
					Source:   "peer",
					Family:   fam,
					LastSeen: lastSeen,
				})
			}
		}
	}

	if len(candidates) > 16 {
		candidates = candidates[:16]
	}
	return candidates
}

// effectivePublicIP returns selected when non-empty, else the first IPv4 candidate,
// else the first IPv6 candidate, else "".
func effectivePublicIP(selected string, candidates []PublicAddress) string {
	if selected != "" {
		return selected
	}
	for _, c := range candidates {
		if c.Family == "ipv4" {
			return c.Address
		}
	}
	for _, c := range candidates {
		if c.Family == "ipv6" {
			return c.Address
		}
	}
	return ""
}

// ConnectAddress is the address a client dials: the connect host when the Owner
// set one, otherwise the public address the Agent reported or the Owner typed.
// Relay, tunnel and canvas endpoints keep using PublicIP, because those are
// addresses the nodes reach each other on and a domain may resolve anywhere.
func (n Node) ConnectAddress() string {
	if n.ConnectHost != "" {
		return n.ConnectHost
	}
	return n.PublicIP
}

// publicIPSource names where the stored public address came from. An empty
// address has no source, so the page can tell "not known yet" from "selected".
func publicIPSource(publicIP, selected string) string {
	switch {
	case publicIP == "":
		return ""
	case selected != "":
		return "selected"
	}
	return "auto"
}

// peerPublicIP is the address the Agent opened the control connection from,
// reported only when it is public by the rule the Agent applies to its own
// interfaces. It is the fallback for a node whose routing table hides the
// default route. The address is observed, not supplied, so it cannot be spoofed
// beyond the connection itself.
func peerPublicIP(ctx context.Context) string {
	remote, ok := peer.FromContext(ctx)
	if !ok {
		return ""
	}
	return netaddr.Select([]net.Addr{remote.Addr})
}

// validConnectHost accepts an empty value (unset), an IP literal or a DNS name.
// It is the install-endpoint host rule plus IP literals, because a connect
// address may be a bare IPv6, which the install command can never take.
func validConnectHost(value string) bool {
	return value == "" || net.ParseIP(value) != nil || netaddr.ValidHostName(value)
}

package node

import (
	"context"
	"net"

	"google.golang.org/grpc/peer"

	"proxycontrol/pkg/contracts/netaddr"
)

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
// address has no source, so the page can tell "not known yet" from "manual".
func publicIPSource(publicIP string, manual bool) string {
	switch {
	case publicIP == "":
		return ""
	case manual:
		return "manual"
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

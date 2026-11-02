package published

import (
	"testing"

	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// A user inbound on a node's second core is published when that core serves the
// protocol, although the primary core's list does not contain it.
func TestSupportsInboundAsksTheInboundsCore(t *testing.T) {
	n := node.Node{Capabilities: &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true}}, Cores: []*pb.CoreCapability{
		{Name: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true}}},
		{Name: "sing-box", Protocols: []*pb.ProtocolCapability{{Protocol: "hysteria2", Inbound: true}}},
	}}}
	if !supportsInbound(n, "", "vless") || !supportsInbound(n, "sing-box", "hysteria2") {
		t.Fatal("an inbound served by its own core was refused")
	}
	if supportsInbound(n, "", "hysteria2") || supportsInbound(n, "sing-box", "vless") {
		t.Fatal("an inbound was accepted by a core that does not serve it")
	}
}

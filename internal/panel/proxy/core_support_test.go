package proxy

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// The panel refuses a protocol the node's own official core cannot carry, from
// the same per-core basis the API and the UI read (plan 6.5).
func TestNodeCoreProtocolSupportRejectsUpstreamUnsupportedDirections(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	record := func(nodeID, core string) {
		t.Helper()
		if _, err := f.nodes.Record(ctx, nodeID, &pb.AgentMessage{NodeId: nodeID, MessageId: "capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: core}}}); err != nil {
			t.Fatal(err)
		}
	}
	saveInbound := func(nodeID, protocol string, port int, relay bool) (Inbound, error) {
		return f.service.SaveInbound(ctx, "", InboundInput{Name: protocol + " entry", NodeID: nodeID, Protocol: protocol, Listen: "0.0.0.0", Port: port, Enabled: true, InternalRelay: relay, DefaultOutbound: "direct"})
	}
	record(f.nodeA, "sing-box")
	record(f.nodeB, "xray")
	third, _, _, err := f.nodes.CreateEnrollment(ctx, "Node C", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.nodes.SetPublicIP(ctx, third.ID, "192.0.2.13"); err != nil {
		t.Fatal(err)
	}

	// WireGuard: official sing-box has no inbound (plan D4), Xray does.
	if _, err := saveInbound(f.nodeA, "wireguard", 24001, false); err == nil || !strings.Contains(err.Error(), "sing-box core") {
		t.Fatal("sing-box node accepted a WireGuard inbound", err)
	}
	if _, err := saveInbound(f.nodeB, "wireguard", 24002, false); err != nil {
		t.Fatal("xray node rejected its WireGuard inbound", err)
	}
	// QUIC protocols: sing-box only.
	if _, err := saveInbound(f.nodeB, "hysteria2", 24003, false); err == nil || !strings.Contains(err.Error(), "xray core") {
		t.Fatal("xray node accepted a hysteria2 inbound", err)
	}
	if _, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "hysteria out", NodeID: f.nodeB, Protocol: "hysteria2", Address: "192.0.2.30", Port: 443, Enabled: true, SupportTCP: true, SupportUDP: true}); err == nil || !strings.Contains(err.Error(), "xray core") {
		t.Fatal("xray node accepted a hysteria2 outbound", err)
	}
	// The same node accepts what its core does carry.
	if _, err := saveInbound(f.nodeA, "vless", 24004, false); err != nil {
		t.Fatal("sing-box node rejected its VLESS inbound", err)
	}
	if _, err := saveInbound(f.nodeB, "socks5", 24005, false); err != nil {
		t.Fatal("xray node rejected its SOCKS5 inbound", err)
	}
	// A node that has not reported a core the matrix describes is left to its
	// runtime capability report.
	if _, err := saveInbound(third.ID, "hysteria2", 24006, true); err != nil {
		t.Fatal("unreported core rejected a protocol before its capabilities arrived", err)
	}
}

// GOST carries forward inbounds only. The panel resolves the selected enabled
// core and refuses every other protocol from the same matrix row, so adding a
// forward row alone would not be enough (docs/v1.3-design-gost.md 3.4).
func TestGostCoreIsForwardOnly(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.nodes.Record(ctx, f.nodeA, &pb.AgentMessage{NodeId: f.nodeA, MessageId: "capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", MultiCoreSupported: true, Cores: []*pb.CoreCapability{
		{Name: "xray", Version: "26.3.27", Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true}}},
		{Name: "gost", Version: "3.3.2", ForwardInboundSupported: true, Protocols: []*pb.ProtocolCapability{{Protocol: "forward", Inbound: true, Tcp: true, Udp: true}}},
	}}}}); err != nil {
		t.Fatal(err)
	}
	forward := func(port int) InboundInput {
		return InboundInput{Name: "gost forward", NodeID: f.nodeA, Core: "gost", Protocol: "forward", Listen: "0.0.0.0", Port: port, Enabled: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp,udp"}}}
	}
	if _, err := f.service.SaveInbound(ctx, "", forward(24011)); err != nil {
		t.Fatalf("gost forward refused: %v", err)
	}
	// This node's Agent does advertise VLESS, but not on GOST.
	user := forward(24012)
	user.Protocol, user.Settings = "vless", desiredstate.ProtocolSettings{}
	if _, err := f.service.SaveInbound(ctx, "", user); err == nil || !strings.Contains(err.Error(), "gost core") {
		t.Fatalf("gost accepted a vless inbound: %v", err)
	}
	// A forward is only selectable for a node that reports the GOST core.
	if _, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "unreported gost", NodeID: f.nodeB, Core: "gost", Protocol: "forward", Listen: "0.0.0.0", Port: 24013, Enabled: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp"}}}); err == nil || !strings.Contains(err.Error(), "does not advertise forward inbound") {
		t.Fatalf("unreported gost core accepted a forward: %v", err)
	}
}

package proxy

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
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

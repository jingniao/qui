package proxy

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// gostForwardInput is one GOST forward with G2 options: a two-target pool, a
// range, a fifo selector and limits.
func gostForwardInput(nodeID string, port, end int) InboundInput {
	strategy, failTimeout := "fifo", "30s"
	maxFails, rate := 2, int64(1048576)
	return InboundInput{
		Name: "gost forward", NodeID: nodeID, Core: "gost", Protocol: "forward", Listen: "0.0.0.0", Port: port, Enabled: true, DefaultOutbound: "direct",
		Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{
			Network:        "tcp,udp",
			Targets:        []desiredstate.ForwardTarget{{Host: "203.0.113.10", Port: 8443}, {Host: "203.0.113.11", Port: 8443}},
			Strategy:       &strategy,
			MaxFails:       &maxFails,
			FailTimeout:    &failTimeout,
			PortEnd:        &end,
			BandwidthInBps: &rate,
		}},
	}
}

// TestGostForwardOptionsRequireTheContractAndTheCore proves the panel refuses the
// G2 forward options unless the selected core is a GOST core that reports settings
// contract 2, and that a range reserves every number it listens on node-wide.
func TestGostForwardOptionsRequireTheContractAndTheCore(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	record := func(nodeID string, gostVersion uint32) {
		t.Helper()
		capabilities := &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, Cores: []*pb.CoreCapability{
			{Name: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "socks5", Inbound: true}}, ForwardInboundSupported: true},
			{Name: "gost", Version: "3.3.2", ForwardInboundSupported: true, GostContractVersion: gostVersion, Protocols: []*pb.ProtocolCapability{{Protocol: "forward", Inbound: true, Tcp: true, Udp: true}}},
		}}
		if _, err := f.nodes.Record(ctx, nodeID, &pb.AgentMessage{NodeId: nodeID, MessageId: "capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: capabilities}}); err != nil {
			t.Fatal(err)
		}
	}
	record(f.nodeA, 1)
	record(f.nodeB, desiredstate.GostForwardContractVersion)
	// An Agent that only reports the G1 contract cannot render a pool or a range.
	if _, err := f.service.SaveInbound(ctx, "", gostForwardInput(f.nodeA, 24100, 24102)); err == nil || !strings.Contains(err.Error(), "does not report forward") {
		t.Fatalf("gost contract 1 accepted the G2 options: %v", err)
	}
	// The same options need the GOST core: the xray core renders one target and
	// no range, so accepting them would silently drop the Owner's settings.
	onXray := gostForwardInput(f.nodeB, 24100, 24102)
	onXray.Core = "xray"
	if _, err := f.service.SaveInbound(ctx, "", onXray); err == nil || !strings.Contains(err.Error(), "require the gost core") {
		t.Fatalf("xray core accepted the G2 options: %v", err)
	}
	saved, err := f.service.SaveInbound(ctx, "", gostForwardInput(f.nodeB, 24100, 24102))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Settings.Forward == nil || len(saved.Settings.Forward.Targets) != 2 || saved.Settings.Forward.PortEnd == nil || *saved.Settings.Forward.PortEnd != 24102 {
		t.Fatalf("the pool did not survive the save: %+v", saved.Settings.Forward)
	}
	// A range reserves every number it listens on, not just its base port.
	user := InboundInput{Name: "inside the range", NodeID: f.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 24101, Enabled: true, DefaultOutbound: "direct"}
	if _, err := f.service.SaveInbound(ctx, "", user); err == nil || !strings.Contains(err.Error(), "24101") {
		t.Fatalf("a port inside the range was accepted: %v", err)
	}
	user.Port = 24103
	if _, err := f.service.SaveInbound(ctx, "", user); err != nil {
		t.Fatalf("a port next to the range was refused: %v", err)
	}
	// The same rule the other way round: a new range that covers an existing
	// listener is refused, and a number strictly inside it is named.
	beside := user
	beside.Name, beside.Port = "beside the range", 24110
	if _, err := f.service.SaveInbound(ctx, "", beside); err != nil {
		t.Fatal(err)
	}
	cover := gostForwardInput(f.nodeB, 24108, 24112)
	cover.Name = "second gost forward"
	if _, err := f.service.SaveInbound(ctx, "", cover); err == nil || !strings.Contains(err.Error(), "24110") {
		t.Fatalf("a range over an occupied port was accepted: %v", err)
	}
	if _, err := f.service.SaveInbound(ctx, saved.ID, gostForwardInput(f.nodeB, 24100, 24102)); err != nil {
		t.Fatalf("re-saving the forward conflicted with itself: %v", err)
	}
}

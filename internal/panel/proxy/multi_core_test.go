package proxy

import (
	"context"
	"errors"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
)

func TestNoUserCoreInboundAndOutboundAdmission(t *testing.T) {
	caps := &pb.Capabilities{CoreOptionalSupported: true, MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "gost", ForwardInboundSupported: true}}}
	for _, tc := range []struct {
		core, protocol string
		allowed        bool
	}{
		{"gost", "forward", true}, {"", "forward", false}, {"", "socks5", false}, {"xray", "socks5", false}, {"gost", "socks5", false},
	} {
		err := ValidateInboundCore(caps, tc.core, tc.protocol)
		if tc.allowed && err != nil || !tc.allowed && !errors.Is(err, node.ErrNoUserCore) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if err := ValidateInboundCore(&pb.Capabilities{}, "", "socks5"); err != nil {
		t.Fatal("legacy unknown report changed", err)
	}
	caps.Cores = nil
	if err := ValidateInboundCore(caps, "gost", "forward"); err == nil {
		t.Fatal("absent GOST accepted")
	}
	f := setup(t)
	ctx := context.Background()
	if _, err := f.nodes.Record(ctx, f.nodeA, &pb.AgentMessage{NodeId: f.nodeA, Sequence: 1, MessageId: "empty", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: caps}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveOutbound(ctx, "", OutboundInput{NodeID: f.nodeA, Name: "proxy", Protocol: "socks5", Address: "example.com", Port: 1080, Enabled: true}); !errors.Is(err, node.ErrNoUserCore) {
		t.Fatal(err)
	}
}

func TestInboundSelectedCorePersistsAndValidatesCapabilities(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	c := &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "xray", DeviceGateSupported: true}, {Name: "sing-box"}}}
	if _, err := f.nodes.Record(ctx, f.nodeA, &pb.AgentMessage{NodeId: f.nodeA, Sequence: 1, MessageId: "cores", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: c}}); err != nil {
		t.Fatal(err)
	}
	in := InboundInput{Core: "sing-box", Name: "secondary", NodeID: f.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 28888, Enabled: true, DefaultOutbound: "direct"}
	saved, err := f.service.SaveInbound(ctx, "", in)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Core != "sing-box" || saved.UserTrafficSupported == nil || *saved.UserTrafficSupported || saved.DeviceGateSupported == nil || *saved.DeviceGateSupported {
		t.Fatal(saved)
	}
	state, _, err := f.service.SnapshotIntent(ctx, f.nodeA, f.users)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Inbounds) != 1 || state.Inbounds[0].Core != "sing-box" {
		t.Fatal(state.Inbounds)
	}
	in.Core = "xray"
	if _, err := f.service.SaveInbound(ctx, "", in); err == nil {
		t.Fatal("cross-core port conflict accepted")
	}
	in.Core = "gost"
	in.Port++
	if _, err := f.service.SaveInbound(ctx, "", in); err == nil {
		t.Fatal("absent core accepted")
	}
	in.Core = "sing-box"
	in.Protocol = "wireguard"
	if _, err := f.service.SaveInbound(ctx, "", in); err == nil {
		t.Fatal("WireGuard accepted on sing-box")
	}
	in.Protocol = "forward"
	if _, err := f.service.SaveInbound(ctx, "", in); err == nil {
		t.Fatal("forward capability ignored")
	}
}

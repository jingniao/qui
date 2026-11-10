package proxy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func hopInbound(port, start, end int) InboundInput {
	return InboundInput{
		Name: "hy2 hop", Protocol: "hysteria2", Listen: "0.0.0.0", Port: port, Enabled: true,
		DefaultOutbound: "direct",
		Settings: desiredstate.ProtocolSettings{
			Transport: "quic", Security: "tls",
			CertificatePath: desiredstate.CertificateDirectory + "/a.crt",
			PrivateKeyPath:  desiredstate.CertificateDirectory + "/a.key",
			QUIC:            &desiredstate.QUICSettings{HopPortStart: start, HopPortEnd: end, HopIntervalSeconds: 30},
		},
	}
}

func recordHopCapabilities(t *testing.T, f *fixture, nodeID string, supported bool, sequence uint64) {
	t.Helper()
	caps := &pb.Capabilities{
		CoreName: "sing-box", CoreVersion: "1.14.3", MultiCoreSupported: true,
		TlsCertificateFilesSupported: true,
		Cores: []*pb.CoreCapability{{Name: "sing-box", Version: "1.14.3", TlsCertificateFilesSupported: true, Protocols: []*pb.ProtocolCapability{{Protocol: "hysteria2", Inbound: true, Udp: true}}}},
		PortHopSupported: supported,
	}
	msg := &pb.AgentMessage{NodeId: nodeID, MessageId: "capabilities-hop", Sequence: sequence, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: caps}}
	if _, err := f.nodes.Record(context.Background(), nodeID, msg); err != nil {
		t.Fatal(err)
	}
}

// An Agent without the host-network helper cannot maintain the hop table, so
// the panel refuses to save the inbound instead of publishing fields it drops.
func TestSaveInboundRefusesHopWithoutAgentSupport(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	recordHopCapabilities(t, f, f.nodeA, false, 1)
	input := hopInbound(8443, 20000, 20010)
	input.NodeID = f.nodeA
	if _, err := f.service.SaveInbound(ctx, "", input); err == nil || !errors.Is(err, desiredstate.ErrPortHopUnsupported) {
		t.Fatalf("old Agent accepted a hop inbound: %v", err)
	}
	recordHopCapabilities(t, f, f.nodeA, true, 2)
	if _, err := f.service.SaveInbound(ctx, "", input); err != nil {
		t.Fatalf("capable Agent refused a hop inbound: %v", err)
	}
}

// The hop range occupies every number in it: a second inbound listening inside
// the first one's range is reported as a conflict, exactly like a real listener.
func TestHopRangeOccupiesItsPorts(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	recordHopCapabilities(t, f, f.nodeA, true, 1)
	first := hopInbound(8443, 20000, 20010)
	first.NodeID = f.nodeA
	if _, err := f.service.SaveInbound(ctx, "", first); err != nil {
		t.Fatal(err)
	}
	second := hopInbound(20005, 30000, 30010)
	second.NodeID = f.nodeA
	if _, err := f.service.SaveInbound(ctx, "", second); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("a listener inside the hop range was not reported as occupied: %v", err)
	}
}

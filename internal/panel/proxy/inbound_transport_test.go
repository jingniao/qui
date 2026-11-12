package proxy

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// HTTP, SOCKS5 and Shadowsocks are TCP only: the save, the snapshot and the
// pre-publish gate (all through ValidateInboundServerSettings) refuse a transport
// the cores cannot render or the client links cannot describe.
func TestInboundTransportRuleOnSaveAndSnapshot(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	port := 25000
	for _, protocol := range []string{"http", "socks5", "shadowsocks"} {
		for _, settings := range []desiredstate.ProtocolSettings{
			{Transport: "ws", Path: "/ws"},
			{Transport: "grpc", ServiceName: "svc"},
			{Transport: desiredstate.TransportXHTTP, XHTTP: &desiredstate.XHTTPSettings{Path: "/x", Mode: desiredstate.XHTTPModeAuto}},
		} {
			port++
			_, err := f.service.SaveInbound(ctx, "", InboundInput{Name: protocol + "-bad", NodeID: f.nodeA, Protocol: protocol, Listen: "0.0.0.0", Port: port, Enabled: true, DefaultOutbound: "direct", Settings: settings})
			if err == nil || !strings.Contains(err.Error(), "只支持 TCP") {
				t.Fatalf("%s with %s saved: %v", protocol, settings.Transport, err)
			}
			if got := ValidateInboundServerSettings(nil, "", protocol, settings); got == nil || !strings.Contains(got.Error(), "只支持 TCP") {
				t.Fatalf("pre-publish gate accepted %s with %s: %v", protocol, settings.Transport, got)
			}
		}
	}
	// VLESS and Trojan keep WebSocket and gRPC.
	for _, protocol := range []string{"vless", "trojan"} {
		for _, settings := range []desiredstate.ProtocolSettings{{Transport: "ws", Path: "/ws"}, {Transport: "grpc", ServiceName: "svc"}} {
			if err := ValidateInboundServerSettings(nil, "", protocol, settings); err != nil {
				t.Fatalf("%s with %s refused: %v", protocol, settings.Transport, err)
			}
		}
	}

	// A stored legacy row is refused by the snapshot (the publish path).
	sealed, err := f.service.Repository.encode(desiredstate.ProtocolSettings{Transport: "ws", Path: "/ws"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Repository.Database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at,core) VALUES('legacy-http','legacy-http',?,'http','0.0.0.0',25100,0,0,0,'direct','[]',?,'2026-11-05T00:00:00Z','xray')", f.nodeA, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.nodes.Record(ctx, f.nodeA, &pb.AgentMessage{NodeId: f.nodeA, MessageId: "capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", Protocols: []*pb.ProtocolCapability{{Protocol: "http", Inbound: true, Outbound: true, Tcp: true}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Snapshot(ctx, f.nodeA, f.users); err == nil || !strings.Contains(err.Error(), "只支持 TCP") {
		t.Fatalf("snapshot carried a legacy http+ws inbound: %v", err)
	}
}

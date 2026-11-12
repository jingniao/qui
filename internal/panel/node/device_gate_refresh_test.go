package node

import (
	"context"
	"google.golang.org/protobuf/types/known/timestamppb"
	"path/filepath"
	"proxycontrol/internal/testutil/dbtest"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

// A GOST primary carries no user access, so its scalar capability says nothing
// about gating. The refresh decision reads every core, exactly like the Hello the
// Agent sends: a node gating through Xray or sing-box keeps receiving gates.
func TestGostPrimaryStillRefreshesDeviceGates(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	n, _, _, err := r.CreateEnrollment(ctx, "gost primary", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	caps := &pb.Capabilities{CoreName: "gost", MultiCoreSupported: true, Cores: []*pb.CoreCapability{
		{Name: "gost", Version: "3.3.2", ForwardInboundSupported: true, GostContractVersion: 3, Protocols: []*pb.ProtocolCapability{{Protocol: "forward", Inbound: true, Tcp: true, Udp: true}}},
		{Name: "xray", Version: "26.3.27", DeviceGateSupported: true, ObservationContractVersion: 2, Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true}}},
	}}
	if err := validateMultiCapabilities(caps); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Record(ctx, n.ID, &pb.AgentMessage{NodeId: n.ID, Sequence: 1, MessageId: "caps", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: caps}}); err != nil {
		t.Fatal(err)
	}
	current, err := r.Get(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Capabilities.GetDeviceGateSupported() {
		t.Fatal("the GOST primary invented a gating capability")
	}
	if !AnyGateSupported(current.Capabilities) {
		t.Fatal("the stored report of a gating secondary core lost its gates")
	}
}

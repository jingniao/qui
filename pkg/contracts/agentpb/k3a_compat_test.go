package agentpb_test

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
)

func TestK3aWireIsAdditive(t *testing.T) {
	file := protodesc.ToFileDescriptorProto(pb.File_agent_proto)
	limits := map[string]int32{"Heartbeat": 32, "Capabilities": 20, "ServerMessage": 20, "AgentMessage": 27, "Hello": 10, "Welcome": 19, "RollbackCommand": 3, "ApplyResult": 6, "HealthCheckCommand": 3, "HealthCheckResult": 5, "CoreUpdateCommand": 1, "CoreUpdateResult": 1, "ConnectionStatus": 1}
	for _, m := range file.MessageType {
		limit, ok := limits[m.GetName()]
		if !ok {
			continue
		}
		kept := m.Field[:0]
		lastOneof := int32(-1)
		for _, f := range m.Field {
			if f.GetNumber() <= limit {
				kept = append(kept, f)
				if f.OneofIndex != nil && f.GetOneofIndex() > lastOneof {
					lastOneof = f.GetOneofIndex()
				}
			}
		}
		m.Field = kept
		m.OneofDecl = m.OneofDecl[:lastOneof+1]
	}
	oldFile, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []proto.Message{
		&pb.Hello{MultiCoreSupported: true, NodeTrafficSupported: true}, &pb.Welcome{MultiCoreSupported: true, NodeTrafficSupported: true},
		&pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "xray"}, {Name: "sing-box", SingboxUserStats: true}}},
		&pb.Heartbeat{Cores: []*pb.CoreStatus{{Name: "xray", Healthy: true}, {Name: "sing-box", Error: "core_unhealthy"}}},
		&pb.RollbackCommand{Core: "xray"}, &pb.ApplyResult{Core: "xray"}, &pb.HealthCheckCommand{Core: "sing-box"}, &pb.HealthCheckResult{Core: "sing-box"},
		&pb.ConnectionStatus{Cores: []*pb.CoreConnectionStatus{{Name: "xray", SnapshotJson: []byte(`{}`)}}},
		&pb.CoreUpdateCommand{Component: "sing-box"}, &pb.CoreUpdateResult{Component: "sing-box"},
		&pb.ServerMessage{Payload: &pb.ServerMessage_RemoveCoreCommand{RemoveCoreCommand: &pb.RemoveCoreCommand{Id: "r", Component: "sing-box", Revision: 1}}},
		&pb.AgentMessage{Payload: &pb.AgentMessage_RemoveCoreResult{RemoveCoreResult: &pb.RemoveCoreResult{Id: "r", Component: "sing-box", Success: true}}},
	} {
		old := dynamicpb.NewMessage(oldFile.Messages().ByName(m.ProtoReflect().Descriptor().Name()))
		raw, err := proto.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := proto.Unmarshal(raw, old); err != nil || len(old.GetUnknown()) == 0 {
			t.Fatal("old peer did not preserve unknown fields", err)
		}
		round, err := proto.Marshal(old)
		if err != nil {
			t.Fatal(err)
		}
		restored := m.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(round, restored); err != nil || !proto.Equal(m, restored) {
			t.Fatal("round trip lost additive fields", err)
		}
	}
	if (&pb.Hello{}).MultiCoreSupported || (&pb.Welcome{}).MultiCoreSupported || (&pb.Capabilities{}).MultiCoreSupported || len((&pb.Heartbeat{}).Cores) > 0 {
		t.Fatal("old peer fabricated multi-core support")
	}
}

package agentpb_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestK2aWireIsAdditive(t *testing.T) {
	file := protodesc.ToFileDescriptorProto(pb.File_agent_proto)
	limits := map[string]int32{"Heartbeat": 22, "Capabilities": 18, "ServerMessage": 19, "AgentMessage": 26}
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
		&pb.Capabilities{Host: &pb.HostInfo{CpuModel: "fixture", CpuCores: 4, Architecture: "arm64", KernelVersion: "6", MemoryTotalBytes: 100, SwapTotalBytes: 200, DiskTotalBytes: 300, OsPrettyName: "Fixture"}, TuningSupported: true},
		&pb.Heartbeat{Load1: proto.Float64(0), Load5: proto.Float64(1), Load15: proto.Float64(2), ProcessCount: proto.Uint64(3), TcpConnections: proto.Uint64(4), UdpSockets: proto.Uint64(5), MemoryUsedBytes: proto.Uint64(6), SwapUsedBytes: proto.Uint64(0), DiskUsedBytes: proto.Uint64(8), TuningCurrent: map[string]string{"net.ipv4.tcp_ecn": "1"}},
		&pb.ServerMessage{Payload: &pb.ServerMessage_TuningCommand{TuningCommand: &pb.TuningCommand{RequestId: "r", Settings: map[string]string{"net.ipv4.tcp_ecn": "1"}}}},
		&pb.AgentMessage{Payload: &pb.AgentMessage_TuningResult{TuningResult: &pb.TuningResult{RequestId: "r", Status: "applied", Keys: []*pb.TuningKeyResult{{Key: "net.ipv4.tcp_ecn", Value: "1", Applied: true}}}}},
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
	old := dynamicpb.NewMessage(oldFile.Messages().ByName("Heartbeat"))
	raw, _ := proto.Marshal(old)
	h := &pb.Heartbeat{}
	if err := proto.Unmarshal(raw, h); err != nil {
		t.Fatal(err)
	}
	if h.Load1 != nil || h.ProcessCount != nil || h.TcpConnections != nil || h.UdpSockets != nil || h.MemoryUsedBytes != nil || h.SwapUsedBytes != nil || h.DiskUsedBytes != nil || len(h.TuningCurrent) != 0 {
		t.Fatal("old peer fabricated metrics")
	}
	if (&pb.Capabilities{}).TuningSupported || (&pb.Capabilities{}).Host != nil {
		t.Fatal("old peer advertises K2a")
	}
}

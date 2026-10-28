package agentpb

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	"testing"
)

func TestHeartbeatHostMetricsAreAdditiveTo109(t *testing.T) {
	descriptor := protodesc.ToFileDescriptorProto(File_agent_proto)
	for _, message := range descriptor.MessageType {
		if message.GetName() == "Heartbeat" {
			kept := message.Field[:0]
			for _, field := range message.Field {
				if field.GetNumber() <= 17 {
					kept = append(kept, field)
				}
			}
			message.Field = kept
			message.OneofDecl = message.OneofDecl[:5]
		}
	}
	oldFile, err := protodesc.NewFile(descriptor, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	old := dynamicpb.NewMessage(oldFile.Messages().ByName("Heartbeat"))
	current := &Heartbeat{HostUptimeSeconds: proto.Float64(123), TcpCongestionControl: proto.String("bbr"), Interfaces: []*InterfaceCounters{{Name: "eth0", ReceiveBytes: 50}}}
	raw, err := proto.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err = proto.Unmarshal(raw, old); err != nil {
		t.Fatal(err)
	}
	old.Clear(old.Descriptor().Fields().ByNumber(1))
	empty := &Heartbeat{}
	if err = proto.Unmarshal([]byte{}, empty); err != nil {
		t.Fatal(err)
	}
	if empty.HostUptimeSeconds != nil || empty.CoreStartedAt != nil || empty.TcpCongestionControl != nil || len(empty.Interfaces) != 0 {
		t.Fatal("old heartbeat fabricated metrics")
	}
	round, err := proto.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	restored := &Heartbeat{}
	if err = proto.Unmarshal(round, restored); err != nil || restored.GetHostUptimeSeconds() != 123 {
		t.Fatal(restored, err)
	}
}

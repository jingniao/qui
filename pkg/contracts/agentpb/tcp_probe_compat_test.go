package agentpb_test

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
)

func TestTCPProbeWireIsAdditiveAndOldPeersCannotAdvertise(t *testing.T) {
	file := protodesc.ToFileDescriptorProto(pb.File_agent_proto)
	remove := map[string]int32{"Hello": 10, "ServerMessage": 19, "AgentMessage": 26, "Capabilities": 18}
	for _, message := range file.MessageType {
		for i, f := range message.Field {
			if f.GetNumber() == remove[message.GetName()] {
				message.Field = append(message.Field[:i], message.Field[i+1:]...)
				break
			}
		}
	}
	oldFile, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []proto.Message{&pb.Capabilities{ForwardInboundSupported: true}, &pb.Hello{TcpProbeSupported: true}, &pb.ServerMessage{Payload: &pb.ServerMessage_TcpProbeCommand{TcpProbeCommand: &pb.TCPProbeCommand{Id: "probe"}}}, &pb.AgentMessage{Payload: &pb.AgentMessage_TcpProbeResult{TcpProbeResult: &pb.TCPProbeResult{Id: "probe"}}}} {
		old := dynamicpb.NewMessage(oldFile.Messages().ByName(message.ProtoReflect().Descriptor().Name()))
		raw, _ := proto.Marshal(message)
		if err = proto.Unmarshal(raw, old); err != nil || len(old.GetUnknown()) == 0 {
			t.Fatal("old peer cannot ignore additive operation", err)
		}
	}
	if (&pb.Hello{}).GetTcpProbeSupported() {
		t.Fatal("old peer advertised support")
	}
}

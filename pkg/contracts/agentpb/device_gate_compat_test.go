package agentpb_test

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
)

func TestDeviceGateFlagsAreAdditiveAndOldPeersDefaultFalse(t *testing.T) {
	file := protodesc.ToFileDescriptorProto(pb.File_agent_proto)
	remove := map[string]int32{"Capabilities": 17, "Hello": 9, "Welcome": 19}
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
	for _, item := range []struct {
		name    string
		message proto.Message
		enabled func(proto.Message) bool
	}{{"Capabilities", &pb.Capabilities{CoreName: "xray", DeviceGateSupported: true}, func(m proto.Message) bool { return m.(*pb.Capabilities).DeviceGateSupported }}, {"Hello", &pb.Hello{DeviceGateSupported: true}, func(m proto.Message) bool { return m.(*pb.Hello).DeviceGateSupported }}, {"Welcome", &pb.Welcome{DeviceGateSupported: true}, func(m proto.Message) bool { return m.(*pb.Welcome).DeviceGateSupported }}} {
		old := dynamicpb.NewMessage(oldFile.Messages().ByName(item.message.ProtoReflect().Descriptor().Name()))
		data, _ := proto.Marshal(item.message)
		if proto.Unmarshal(data, old) != nil || len(old.GetUnknown()) == 0 {
			t.Fatal("old peer failed to ignore additive field", item.name)
		}
		old.SetUnknown(nil)
		data, _ = proto.Marshal(old)
		current := item.message.ProtoReflect().New().Interface()
		if proto.Unmarshal(data, current) != nil || item.enabled(current) {
			t.Fatal("old peer acquired gate support", item.name)
		}
	}
}

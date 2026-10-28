package agentpb_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestSingboxStatsCapabilityIsAdditiveForOldPeers(t *testing.T) {
	file := protodesc.ToFileDescriptorProto(pb.File_agent_proto)
	for _, message := range file.MessageType {
		if message.GetName() != "Capabilities" {
			continue
		}
		for i, field := range message.Field {
			if field.GetNumber() == 16 {
				message.Field = append(message.Field[:i], message.Field[i+1:]...)
				break
			}
		}
	}
	oldFile, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	old := dynamicpb.NewMessage(oldFile.Messages().ByName("Capabilities"))
	data, err := proto.Marshal(&pb.Capabilities{CoreName: "sing-box", CoreVersion: "1.14.4", SingboxUserStats: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(data, old); err != nil {
		t.Fatal("old panel rejected additive field", err)
	}
	if old.Get(old.Descriptor().Fields().ByNumber(1)).String() != "sing-box" || len(old.GetUnknown()) == 0 {
		t.Fatal("old panel lost existing fields or did not ignore new field")
	}
	old.SetUnknown(nil)
	data, err = proto.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	current := &pb.Capabilities{}
	if err := proto.Unmarshal(data, current); err != nil {
		t.Fatal(err)
	}
	if current.SingboxUserStats || current.CoreName != "sing-box" {
		t.Fatal("old Agent changed D5 default", current)
	}
}

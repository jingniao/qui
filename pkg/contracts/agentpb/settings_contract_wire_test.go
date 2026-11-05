package agentpb_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func settingsContractCapabilities() *pb.Capabilities {
	return &pb.Capabilities{
		CoreName: "xray", CoreVersion: "26.3.27", MultiCoreSupported: true,
		Cores: []*pb.CoreCapability{
			{
				Name: "xray", Version: "26.3.27", ServerSettingsContractVersion: 1,
				ServerSettingsCombinations: []*pb.SettingsCombination{
					{Protocol: "vless", Direction: "inbound", Security: "reality", Transport: "tcp"},
					{Protocol: "vless", Direction: "inbound", Security: "tls", Transport: "xhttp"},
				},
			},
			{Name: "sing-box", Version: "1.14.3"},
		},
	}
}

// legacyDescriptor is the M1 report as a peer that predates the settings contract
// sees it: CoreCapability stops at gost_contract_version and Capabilities at
// multi_core_supported.
func legacyDescriptor(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	file := protodesc.ToFileDescriptorProto(pb.File_agent_proto)
	for _, message := range file.MessageType {
		limit := int32(0)
		switch message.GetName() {
		case "CoreCapability":
			limit = 9
		case "Capabilities":
			limit = 22
		}
		if limit == 0 {
			continue
		}
		kept := message.Field[:0]
		for _, field := range message.Field {
			if field.GetNumber() <= limit {
				kept = append(kept, field)
			}
		}
		message.Field = kept
	}
	descriptor, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor
}

// The settings contract is ordinary additive protobuf: the binary and protojson
// (the panel's storage format) round trips keep it, an older peer keeps it as
// unknown fields instead of dropping it, and a core that reports nothing stays
// at zero through the same path.
func TestSettingsContractRoundTrip(t *testing.T) {
	capabilities := settingsContractCapabilities()
	raw, err := proto.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	binary := &pb.Capabilities{}
	if err := proto.Unmarshal(raw, binary); err != nil || !proto.Equal(capabilities, binary) {
		t.Fatalf("binary round trip lost the settings contract: %v", err)
	}
	stored, err := protojson.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &pb.Capabilities{}
	if err := protojson.Unmarshal(stored, decoded); err != nil || !proto.Equal(capabilities, decoded) {
		t.Fatalf("stored round trip lost the settings contract: %v", err)
	}
	if decoded.Cores[1].ServerSettingsContractVersion != 0 || len(decoded.Cores[1].ServerSettingsCombinations) != 0 {
		t.Fatalf("a core without a report gained one: %+v", decoded.Cores[1])
	}
	legacy := legacyDescriptor(t)
	old := dynamicpb.NewMessage(legacy.Messages().ByName("Capabilities"))
	if err := proto.Unmarshal(raw, old); err != nil {
		t.Fatal(err)
	}
	cores := old.Get(legacy.Messages().ByName("Capabilities").Fields().ByName("cores")).List()
	if cores.Len() != 2 || len(cores.Get(0).Message().GetUnknown()) == 0 {
		t.Fatalf("an older peer did not keep the settings contract as unknown fields")
	}
	if len(cores.Get(1).Message().GetUnknown()) != 0 {
		t.Fatalf("a core without a report carried unknown fields")
	}
	round, err := proto.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	restored := &pb.Capabilities{}
	if err := proto.Unmarshal(round, restored); err != nil || !proto.Equal(capabilities, restored) {
		t.Fatalf("an older peer dropped the settings contract: %v", err)
	}
}

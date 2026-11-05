package node

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func reportReality() *pb.SettingsCombination {
	return &pb.SettingsCombination{Protocol: "vless", Direction: desiredstate.DirectionInbound, Security: desiredstate.SecurityReality, Transport: "tcp"}
}

func reportXHTTP() *pb.SettingsCombination {
	return &pb.SettingsCombination{Protocol: "vless", Direction: desiredstate.DirectionInbound, Transport: desiredstate.TransportXHTTP}
}

func reportTLSXHTTP() *pb.SettingsCombination {
	return &pb.SettingsCombination{Protocol: "vless", Direction: desiredstate.DirectionInbound, Security: "tls", Transport: desiredstate.TransportXHTTP}
}

var reportedVLESS = []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true, Udp: true}}

func realityListener() desiredstate.ProtocolSettings {
	return desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: &desiredstate.RealityServerSettings{}}
}

func xhttpListener() desiredstate.ProtocolSettings {
	return desiredstate.ProtocolSettings{Transport: desiredstate.TransportXHTTP, XHTTP: &desiredstate.XHTTPSettings{Path: "/", Mode: desiredstate.XHTTPModeAuto}}
}

// A core's own report is the only evidence: the legacy scalar shape and the
// multi-core shape both carry it, and nothing else stands in for it — not the
// core version text, not the VLESS inbound flag, not another core's report.
func TestSettingsContractComesOnlyFromTheReport(t *testing.T) {
	legacy := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", Protocols: reportedVLESS}
	modern := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", Protocols: reportedVLESS, MultiCoreSupported: true, Cores: []*pb.CoreCapability{
		{Name: "xray", Version: "26.3.27", Protocols: reportedVLESS},
		{Name: "sing-box", Version: "1.14.3", Protocols: reportedVLESS},
	}}
	for name, capabilities := range map[string]*pb.Capabilities{"legacy scalar": legacy, "multi-core": modern} {
		core := SelectedCore(capabilities, "xray")
		if core == nil {
			t.Fatalf("%s: xray core not reported", name)
		}
		capability := SettingsContractCapability(core)
		if capability.Core != "xray" || capability.Version != 0 || len(capability.Combinations) != 0 {
			t.Fatalf("%s: invented a settings contract: %+v", name, capability)
		}
		for _, settings := range []desiredstate.ProtocolSettings{realityListener(), xhttpListener()} {
			if err := desiredstate.RequireInboundServerSettings(core.Name, capability, "vless", settings); err == nil {
				t.Fatalf("%s: the VLESS flag and the version text authorized xhttp/Reality settings", name)
			}
		}
	}
	if capability := SettingsContractCapability(SelectedCore(modern, "sing-box")); capability.Core != "sing-box" || capability.Version != 0 {
		t.Fatalf("sing-box report invented a contract: %+v", capability)
	}
	if capability := SettingsContractCapability(nil); capability.Core != "" || capability.Version != 0 || capability.Combinations != nil {
		t.Fatalf("absent core produced a capability: %+v", capability)
	}
}

// A synthetic exact report reaches the shared gate unchanged and authorizes
// exactly the combinations it lists; every neighbouring case stays closed.
func TestSettingsContractAuthorizesOnlyItsOwnCombination(t *testing.T) {
	capabilities := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", MultiCoreSupported: true, Cores: []*pb.CoreCapability{
		{Name: "xray", Version: "26.3.27", Protocols: reportedVLESS, ServerSettingsContractVersion: desiredstate.SettingsContractVersion,
			ServerSettingsCombinations: []*pb.SettingsCombination{reportReality(), reportXHTTP(), reportTLSXHTTP()}},
		{Name: "sing-box", Version: "1.14.3", Protocols: reportedVLESS},
	}}
	capability := SettingsContractCapability(SelectedCore(capabilities, "xray"))
	if capability.Core != "xray" || capability.Version != desiredstate.SettingsContractVersion {
		t.Fatalf("report not converted: %+v", capability)
	}
	if len(capability.Combinations) != 3 || capability.Combinations[0].String() != "vless/inbound/reality/tcp/none" || capability.Combinations[1].String() != "vless/inbound/none/xhttp/none" {
		t.Fatalf("combinations not converted: %+v", capability.Combinations)
	}
	if err := desiredstate.RequireInboundServerSettings("xray", capability, "vless", realityListener()); err != nil {
		t.Fatalf("the declared Reality combination was refused: %v", err)
	}
	if err := desiredstate.RequireInboundServerSettings("xray", capability, "vless", xhttpListener()); err != nil {
		t.Fatalf("the declared xhttp combination was refused: %v", err)
	}
	tlsXHTTP := xhttpListener()
	tlsXHTTP.Security = "tls"
	tlsXHTTP.XHTTP.Host = "cdn.example.com"
	if err := desiredstate.RequireInboundServerSettings("xray", capability, "vless", tlsXHTTP); err != nil {
		t.Fatalf("the declared TLS xhttp combination was refused: %v", err)
	}
	vision := desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: &desiredstate.RealityServerSettings{}, Flow: "xtls-rprx-vision"}
	if err := desiredstate.RequireInboundServerSettings("xray", capability, "vless", vision); err == nil {
		t.Fatal("an unlisted flow combination was authorized")
	}
	if err := desiredstate.RequireInboundServerSettings("sing-box", capability, "vless", realityListener()); err == nil {
		t.Fatal("xray's report authorized a sing-box listener")
	}
	unknown := capability
	unknown.Version = desiredstate.SettingsContractVersion + 1
	if err := desiredstate.RequireInboundServerSettings("xray", unknown, "vless", realityListener()); err == nil || !strings.Contains(err.Error(), "unknown settings contract version") {
		t.Fatalf("an unknown version was authorized: %v", err)
	}
	outboundOnly := SettingsContractCapability(&pb.CoreCapability{Name: "xray", ServerSettingsContractVersion: desiredstate.SettingsContractVersion,
		ServerSettingsCombinations: []*pb.SettingsCombination{{Protocol: "vless", Direction: desiredstate.DirectionOutbound, Security: desiredstate.SecurityReality, Transport: "tcp"}}})
	if err := desiredstate.RequireInboundServerSettings("xray", outboundOnly, "vless", realityListener()); err == nil {
		t.Fatal("an outbound combination authorized an inbound listener")
	}
}

// A single-core Agent reports the contract in the scalar fields; modeling that as
// one core keeps the meaning for the panel's fallback and for the Agent's report.
func TestLegacyScalarReportKeepsTheContract(t *testing.T) {
	capabilities := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", Protocols: reportedVLESS,
		ServerSettingsContractVersion: desiredstate.SettingsContractVersion,
		ServerSettingsCombinations:    []*pb.SettingsCombination{reportReality()}}
	capability := SettingsContractCapability(SelectedCore(capabilities, ""))
	if capability.Core != "xray" || capability.Version != desiredstate.SettingsContractVersion || len(capability.Combinations) != 1 {
		t.Fatalf("the scalar report was dropped: %+v", capability)
	}
	if err := desiredstate.RequireInboundServerSettings("xray", capability, "vless", realityListener()); err != nil {
		t.Fatalf("the scalar report was not honored: %v", err)
	}
	message := &pb.AgentMessage{NodeId: "node", Sequence: 1, MessageId: "capabilities", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: capabilities}}
	if err := validateMessage("node", message); err != nil {
		t.Fatalf("the panel refused a valid scalar report: %v", err)
	}
}

// A report is stored as reported, so its shape is checked once, with the shared
// combination validator. Unknown versions are accepted and authorize nothing.
func TestMalformedSettingsReportsAreRefused(t *testing.T) {
	tooMany := make([]*pb.SettingsCombination, maxSettingsCombinations+1)
	for index := range tooMany {
		tooMany[index] = reportReality()
	}
	invalid := map[string]*pb.Capabilities{
		"combinations without a version": {CoreName: "xray", ServerSettingsCombinations: []*pb.SettingsCombination{reportReality()}},
		"no direction":                   {CoreName: "xray", ServerSettingsContractVersion: 1, ServerSettingsCombinations: []*pb.SettingsCombination{{Protocol: "vless", Security: "tls", Transport: "tcp"}}},
		"unknown transport":              {CoreName: "xray", ServerSettingsContractVersion: 1, ServerSettingsCombinations: []*pb.SettingsCombination{{Protocol: "vless", Direction: "inbound", Transport: "http3"}}},
		"duplicate combination":          {CoreName: "xray", ServerSettingsContractVersion: 1, ServerSettingsCombinations: []*pb.SettingsCombination{reportReality(), reportReality()}},
		"empty entry":                    {CoreName: "xray", ServerSettingsContractVersion: 1, ServerSettingsCombinations: []*pb.SettingsCombination{nil}},
		"too many combinations":          {CoreName: "xray", ServerSettingsContractVersion: 1, ServerSettingsCombinations: tooMany},
		"per-core combinations without a version": {CoreName: "xray", MultiCoreSupported: true, Cores: []*pb.CoreCapability{
			{Name: "xray", ServerSettingsCombinations: []*pb.SettingsCombination{reportReality()}},
		}},
	}
	for name, capabilities := range invalid {
		message := &pb.AgentMessage{NodeId: "node", Sequence: 1, MessageId: "capabilities", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: capabilities}}
		if err := validateMessage("node", message); err == nil {
			t.Fatalf("%s: accepted as a capability report", name)
		}
	}
	unknown := &pb.Capabilities{CoreName: "xray", ServerSettingsContractVersion: desiredstate.SettingsContractVersion + 1, ServerSettingsCombinations: []*pb.SettingsCombination{reportReality()}}
	message := &pb.AgentMessage{NodeId: "node", Sequence: 1, MessageId: "capabilities", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: unknown}}
	if err := validateMessage("node", message); err != nil {
		t.Fatalf("a future contract version must not disconnect the node: %v", err)
	}
	if err := desiredstate.RequireInboundServerSettings("xray", SettingsContractCapability(SelectedCore(unknown, "")), "vless", realityListener()); err == nil {
		t.Fatal("a future contract version authorized a combination")
	}
}

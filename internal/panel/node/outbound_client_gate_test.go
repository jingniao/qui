package node

import (
	"errors"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func outboundCombinationReport(core string) []*pb.SettingsCombination {
	var report []*pb.SettingsCombination
	for _, c := range desiredstate.OutboundClientCombinations() {
		if core == desiredstate.CoreSingBox && (c.Transport == desiredstate.TransportXHTTP || c.Security == desiredstate.SecurityReality && c.Transport != "tcp") {
			continue
		}
		report = append(report, &pb.SettingsCombination{Protocol: c.Protocol, Direction: c.Direction, Security: c.Security, Transport: c.Transport, Flow: c.Flow})
	}
	return report
}

func TestRequireOutboundClientSupport(t *testing.T) {
	alpn := desiredstate.ProtocolSettings{Security: "tls", ServerName: "site.example", ALPN: []string{"h2"}}
	xhttp := desiredstate.ProtocolSettings{Security: "tls", Transport: desiredstate.TransportXHTTP, ServerName: "site.example", XHTTP: &desiredstate.XHTTPSettings{Path: "/x", Mode: desiredstate.XHTTPModeAuto}}
	legacy := desiredstate.ProtocolSettings{Security: "tls", ServerName: "site.example"}
	xray := func(report bool) *pb.CoreCapability {
		core := &pb.CoreCapability{Name: "xray"}
		if report {
			core.ServerSettingsContractVersion, core.ServerSettingsCombinations = desiredstate.SettingsContractVersion, outboundCombinationReport("xray")
		}
		return core
	}
	singbox := &pb.CoreCapability{Name: "sing-box", ServerSettingsContractVersion: desiredstate.SettingsContractVersion, ServerSettingsCombinations: outboundCombinationReport("sing-box")}
	for _, tc := range []struct {
		name     string
		settings desiredstate.ProtocolSettings
		enabled  bool
		cores    []*pb.CoreCapability
		want     error
	}{
		{"legacy outbound on an old Agent", legacy, true, []*pb.CoreCapability{xray(false)}, nil},
		{"client fields on an old Agent", alpn, true, []*pb.CoreCapability{xray(false)}, ErrOutboundClientUnsupported},
		{"disabled outbound still travels", alpn, false, []*pb.CoreCapability{xray(false)}, ErrOutboundClientUnsupported},
		{"reported", alpn, true, []*pb.CoreCapability{xray(true)}, nil},
		{"gost renders no outbound", alpn, true, []*pb.CoreCapability{xray(true), {Name: "gost"}}, nil},
		{"xhttp with sing-box enabled", xhttp, true, []*pb.CoreCapability{xray(true), singbox}, ErrOutboundClientUnsupported},
		{"xhttp on xray alone", xhttp, true, []*pb.CoreCapability{xray(true)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := desiredstate.State{NodeID: "node", Outbounds: []desiredstate.Outbound{{ID: "out", Protocol: "vless", Enabled: tc.enabled, Settings: tc.settings}}}
			c := &pb.Capabilities{MultiCoreSupported: true, CoreName: "xray", Cores: tc.cores}
			if err := RequireOutboundClientSupport(state, c, "server"); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

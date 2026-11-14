package node

import (
	"errors"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestRequireOutboundSourceSupport(t *testing.T) {
	for _, tc := range []struct {
		name, source, iface, forced, server string
		disabled, supported, helper, gost   bool
		want                                error
	}{
		{name: "no source"},
		{name: "unsupported", source: "192.0.2.2", want: ErrOutboundSourceUnsupported},
		{name: "interface", iface: "eth0", want: ErrOutboundSourceUnsupported},
		{name: "disabled", source: "192.0.2.2", disabled: true},
		{name: "gost ignored", source: "192.0.2.2", supported: true, gost: true},
		{name: "helper missing", source: "192.0.2.2", forced: "192.0.2.3", supported: true, want: ErrHostSourceExemptUnsupported},
		{name: "helper supported", source: "192.0.2.2", forced: "192.0.2.3", supported: true, helper: true},
		{name: "server source", source: "192.0.2.2", server: "192.0.2.2", forced: "192.0.2.3", supported: true},
		{name: "forced source", source: "192.0.2.3", forced: "192.0.2.3", supported: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := desiredstate.State{NodeID: "node", Network: desiredstate.NetworkOrNil(tc.server, "", tc.forced, ""), Outbounds: []desiredstate.Outbound{{ID: "out", Enabled: !tc.disabled, SourceIP: tc.source, SourceInterface: tc.iface}}}
			c := &pb.Capabilities{MultiCoreSupported: true, CoreName: "xray", Cores: []*pb.CoreCapability{{Name: "xray", OutboundSourceSupported: tc.supported}}, HostSourceExtraExemptSupported: tc.helper}
			if tc.gost {
				c.Cores = append(c.Cores, &pb.CoreCapability{Name: "gost"})
			}
			if err := RequireOutboundSourceSupport(state, c, ""); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRequireOutboundSourceSupportMixedCores(t *testing.T) {
	state := desiredstate.State{NodeID: "node", Outbounds: []desiredstate.Outbound{{ID: "out", Enabled: true, SourceInterface: "eth0"}}}
	c := &pb.Capabilities{MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "xray", OutboundSourceSupported: true}, {Name: "sing-box"}}}
	if err := RequireOutboundSourceSupport(state, c, "server"); !errors.Is(err, ErrOutboundSourceUnsupported) {
		t.Fatalf("mixed cores: %v", err)
	}
	c.Cores[1].OutboundSourceSupported = true
	if err := RequireOutboundSourceSupport(state, c, "server"); err != nil {
		t.Fatal(err)
	}
}

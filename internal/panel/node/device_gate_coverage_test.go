package node

import (
	"proxycontrol/pkg/contracts/agentpb"
	"testing"
)

func TestDeviceGateCoverageFollowsTheMeasuredProtocols(t *testing.T) {
	xray := &agentpb.CoreCapability{Name: "xray", DeviceGateSupported: true}
	singbox := &agentpb.CoreCapability{Name: "sing-box", DeviceGateSupported: true}
	old := &agentpb.CoreCapability{Name: "sing-box"}
	cases := []struct {
		core     *agentpb.CoreCapability
		protocol string
		want     string
	}{
		{xray, "vless", "full"}, {xray, "trojan", "full"}, {xray, "shadowsocks", "full"}, {xray, "http", "full"},
		{xray, "socks5", "tcp_only"},
		{singbox, "vless", "full"}, {singbox, "trojan", "full"}, {singbox, "http", "full"},
		{singbox, "socks5", "tcp_only"}, {singbox, "shadowsocks", "tcp_only"}, {singbox, "hysteria2", "tcp_only"}, {singbox, "tuic", "tcp_only"},
		{singbox, "wireguard", "none"}, {xray, "forward", "none"}, {old, "vless", "none"}, {nil, "vless", "none"},
	}
	for _, c := range cases {
		name := "nil"
		if c.core != nil {
			name = c.core.Name
		}
		if got := DeviceGateCoverage(c.core, c.protocol); got != c.want {
			t.Errorf("%s %s: got %s, want %s", name, c.protocol, got, c.want)
		}
	}
}

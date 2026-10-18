package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
)

func TestSnapshotCarriesNodeOutboundNetworkOnlyWhenConfigured(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	plain, err := f.service.Snapshot(ctx, f.nodeA, f.users)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Network != nil {
		t.Fatal("a node without settings published a network block")
	}
	data, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "network") {
		t.Fatalf("unconfigured node serialized a network block: %s", data)
	}
	sourceIP, iface, ipv4, ipv6 := "192.0.2.10", "eth0", "198.51.100.4", "2001:db8::4"
	if err := f.nodes.UpdateSettings(ctx, f.nodeA, node.SettingsInput{Name: "Node A", Enabled: true, OutboundSourceIP: &sourceIP, OutboundInterface: &iface, HostSourceIPv4: &ipv4, HostSourceIPv6: &ipv6}); err != nil {
		t.Fatal(err)
	}
	configured, err := f.service.Snapshot(ctx, f.nodeA, f.users)
	if err != nil {
		t.Fatal(err)
	}
	if configured.Network == nil || configured.Network.OutboundSourceIP != sourceIP || configured.Network.OutboundInterface != iface || configured.Network.HostSourceIPv4 != ipv4 || configured.Network.HostSourceIPv6 != ipv6 {
		t.Fatalf("snapshot lost the node settings: %+v", configured.Network)
	}
	if err := configured.Validate(); err != nil {
		t.Fatalf("snapshot with settings does not validate: %v", err)
	}
	other, err := f.service.Snapshot(ctx, f.nodeB, f.users)
	if err != nil || other.Network != nil {
		t.Fatalf("unrelated node gained settings: %+v %v", other.Network, err)
	}
}

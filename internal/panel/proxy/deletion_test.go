package proxy

import (
	"context"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

// A deleted node cannot receive new proxy configuration and cannot be the target
// of a relay outbound on another node. The nodes here are still enabled, so these
// checks isolate the deletion from the ordinary "disabled node" refusal.
func TestDeletedNodeRejectedByProxyConfiguration(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	target, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "Relay", NodeID: f.nodeB, Protocol: "trojan", Listen: "0.0.0.0", Port: 8443, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.nodes.Database.ExecContext(ctx, "UPDATE nodes SET deleted_at='2026-01-01T00:00:00.000000000Z' WHERE id=?", f.nodeB); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "In", NodeID: f.nodeB, Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, Enabled: true, DefaultOutbound: "direct"}); err == nil {
		t.Fatal("deleted node accepted an inbound")
	}
	if _, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "Relay", NodeID: f.nodeA, Protocol: "trojan", Enabled: true, TargetInboundID: target.ID, SupportTCP: true, SupportUDP: true}); err == nil {
		t.Fatal("outbound accepted a deleted relay target")
	}
	members := []desiredstate.GroupMember{{OutboundID: "direct", Priority: 0, Weight: 1}}
	if _, err := f.service.SaveGroup(ctx, "", GroupInput{Name: "Exit", NodeID: f.nodeB, Strategy: "primary", Enabled: true, Members: members, FailurePolicy: "Block"}); err == nil {
		t.Fatal("deleted node accepted an outbound group")
	}
	if _, err := f.service.SaveChain(ctx, "", ChainInput{Name: "Chain", NodeIDs: []string{f.nodeA, f.nodeB}, OutboundIDs: []string{"missing"}, SupportTCP: true, Enabled: true}); err == nil {
		t.Fatal("deleted node accepted a proxy chain")
	}
}

package proxy

import (
	"context"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestNestedBackupPersistenceOwnershipCyclesAndDeleteProtection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	create := func(name, nodeID string) Outbound {
		t.Helper()
		out, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: name, NodeID: nodeID, Protocol: "direct", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	member := create("Member", f.nodeA)
	other := create("Other node", f.nodeB)
	group, err := f.service.SaveGroup(ctx, "", GroupInput{Name: "Backups", NodeID: f.nodeA, Enabled: true, Strategy: "primary", Members: []desiredstate.GroupMember{{OutboundID: member.ID, Weight: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	input := OutboundInput{Name: "Primary", NodeID: f.nodeA, Protocol: "socks5", Address: "192.0.2.10", Port: 1080, Enabled: true, SupportTCP: true, FailurePolicy: "BackupOutboundGroup", BackupOutboundID: group.ID}
	primary, err := f.service.SaveOutbound(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.service.Repository.Outbound(ctx, primary.ID, false)
	if err != nil || stored.FailurePolicy != "BackupOutboundGroup" || stored.BackupOutboundID != group.ID {
		t.Fatal("new mode not persisted", stored, err)
	}
	if err := f.service.DeleteGroup(ctx, group.ID); err == nil {
		t.Fatal("deleted referenced backup group")
	}
	input.FailurePolicy = "BackupOutbound"
	input.BackupOutboundID = other.ID
	if _, err := f.service.SaveOutbound(ctx, primary.ID, input); err == nil {
		t.Fatal("cross-node fallback accepted")
	}
	if _, err := f.service.SaveGroup(ctx, group.ID, GroupInput{Name: group.Name, NodeID: f.nodeA, Enabled: true, Strategy: "primary", Members: group.Members, FailurePolicy: "BackupOutbound", BackupOutboundID: primary.ID}); err == nil {
		t.Fatal("group/outbound fallback cycle accepted")
	}
	if _, err := f.service.Repository.Outbound(ctx, primary.ID, false); err != nil {
		t.Fatal("failed edit damaged original")
	}
}

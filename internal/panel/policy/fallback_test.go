package policy

import (
	"context"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
	"time"
)

func TestPolicyBackupGroupTransportOwnerAndSnapshot(t *testing.T) {
	s, nodeID, _, _, _ := fixture(t)
	ctx := context.Background()
	proxies := &proxy.Service{Repository: s.Proxies.(*proxy.Repository), Nodes: s.Nodes.(*node.Repository)}
	out, err := proxies.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "TCP proxy", NodeID: nodeID, Protocol: "http", Address: "192.0.2.10", Port: 8080, Enabled: true, SupportTCP: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := proxies.SaveGroup(ctx, "", proxy.GroupInput{Name: "Backup", NodeID: nodeID, Enabled: true, Strategy: "primary", Members: []desiredstate.GroupMember{{OutboundID: out.ID, Weight: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Name: "Backup policy", Enabled: true, Match: desiredstate.Match{Network: "tcp"}, Action: desiredstate.Action{Type: "Outbound", TargetID: out.ID, Failover: "BackupOutboundGroup", BackupID: pool.ID}}
	rule, err := s.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := s.ForNode(ctx, nodeID)
	if err != nil || len(rules) != 1 || rules[0].Action.BackupID != pool.ID || rules[0].ID != rule.ID {
		t.Fatal("backup not in snapshot", rules, err)
	}
	input.Match.Network = "udp"
	if _, err := s.Save(ctx, "", input); err == nil {
		t.Fatal("UDP policy accepted TCP-only backup")
	}
	other, _, _, err := s.Nodes.(*node.Repository).CreateEnrollment(ctx, "Other", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	otherOut, err := proxies.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "Other direct", NodeID: other.ID, Protocol: "direct", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	input.Match.Network = "tcp"
	input.Action.Failover = "BackupOutbound"
	input.Action.BackupID = otherOut.ID
	if _, err := s.Save(ctx, "", input); err == nil {
		t.Fatal("policy backup crossed entry node")
	}
}

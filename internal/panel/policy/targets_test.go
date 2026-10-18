package policy

import (
	"context"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
	shared "proxycontrol/pkg/contracts/policy"
	"testing"
	"time"
)

func TestGroupPolicyOwnershipTransportSimulationAndDeletion(t *testing.T) {
	s, nodeID, _, _, _ := fixture(t)
	ctx := context.Background()
	r := s.Proxies.(*proxy.Repository)
	proxies := &proxy.Service{Repository: r, Nodes: s.Nodes.(*node.Repository), Policies: s}
	first, err := proxies.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "HTTP primary", NodeID: nodeID, Protocol: "http", Address: "192.0.2.10", Port: 8080, Enabled: true, SupportTCP: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := proxies.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "HTTP secondary", NodeID: nodeID, Protocol: "http", Address: "192.0.2.11", Port: 8080, Enabled: true, SupportTCP: true})
	if err != nil {
		t.Fatal(err)
	}
	group, err := proxies.SaveGroup(ctx, "", proxy.GroupInput{Name: "Exit pool", NodeID: nodeID, Strategy: "round_robin", Enabled: true, Members: []desiredstate.GroupMember{{OutboundID: first.ID, Weight: 1}, {OutboundID: second.ID, Weight: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Name: "TCP service pool", Enabled: true, Match: desiredstate.Match{Domains: []string{"example.test"}, Network: "udp"}, Action: desiredstate.Action{Type: "OutboundGroup", TargetID: group.ID}}
	if _, err := s.Save(ctx, "", input); err == nil {
		t.Fatal("UDP policy accepted HTTP-only member")
	}
	input.Match.Network = "tcp"
	rule, err := s.Save(ctx, "", input)
	if err != nil || len(rule.Scope.NodeIDs) != 1 || rule.Scope.NodeIDs[0] != nodeID {
		t.Fatal("group owner not bound", rule, err)
	}
	other, _, _, err := s.Nodes.(*node.Repository).CreateEnrollment(ctx, "Other entry", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	input.Scope.NodeIDs = []string{other.ID}
	if _, err := s.Save(ctx, "", input); err == nil {
		t.Fatal("group attached to another entry node")
	}
	decision, err := s.Simulate(ctx, shared.Request{NodeID: nodeID, Domain: "example.test", Port: 443, Network: "tcp"})
	if err != nil || !decision.Matched || decision.SelectionStrategy != "round_robin" || len(decision.CandidatePaths) != 2 || decision.FinalEgressNodeID != "" {
		t.Fatal("group simulator invented an exact connection choice", decision, err)
	}
	if err := proxies.DeleteGroup(ctx, group.ID); err == nil {
		t.Fatal("deleted policy group target")
	}
	if err := proxies.DeleteOutbound(ctx, first.ID); err == nil {
		t.Fatal("deleted group member")
	}
	if _, err := proxies.SaveGroup(ctx, group.ID, proxy.GroupInput{Name: group.Name, NodeID: nodeID, Strategy: "primary", Enabled: false, Members: group.Members}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ForNode(ctx, nodeID); err == nil {
		t.Fatal("publication ignored disabled policy group target")
	}
}

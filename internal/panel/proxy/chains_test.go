package proxy

import (
	"context"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestChainCapabilitiesDedicatedBindingAndSnapshotOrder(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	c, _, _, err := f.nodes.CreateEnrollment(ctx, "Node C", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.nodes.SetPublicIP(ctx, c.ID, "192.0.2.12"); err != nil {
		t.Fatal(err)
	}
	ids := []string{f.nodeA, f.nodeB, c.ID}
	for _, nodeID := range ids {
		_, err := f.nodes.Record(ctx, nodeID, &pb.AgentMessage{Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "Xray", Protocols: []*pb.ProtocolCapability{{Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true, Udp: true}}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	inbounds := []Inbound{}
	outboundIDs := []string{}
	for index := 0; index < 2; index++ {
		inbound, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "Internal", NodeID: ids[index+1], Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
		if err != nil {
			t.Fatal(err)
		}
		inbounds = append(inbounds, inbound)
		outbound, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "Next hop", NodeID: ids[index], Protocol: "socks5", TargetInboundID: inbound.ID, Enabled: true, SupportTCP: true, SupportUDP: true})
		if err != nil {
			t.Fatal(err)
		}
		outboundIDs = append(outboundIDs, outbound.ID)
	}
	input := ChainInput{Name: "Three nodes", NodeIDs: ids, OutboundIDs: outboundIDs, Enabled: true, SupportTCP: true, SupportUDP: true}
	chain, err := f.service.SaveChain(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	states, err := f.service.ChainSnapshots(ctx, chain.ID, f.users)
	if err != nil || len(states) != 3 {
		t.Fatal(err)
	}
	for index, snapshot := range states {
		if snapshot.State.NodeID != ids[2-index] {
			t.Fatal("snapshots not exit first")
		}
		if snapshot.State.NodeID == f.nodeB {
			matched := false
			for _, outbound := range snapshot.State.Outbounds {
				if outbound.ID == snapshot.State.Inbounds[0].DefaultOutbound && outbound.TargetNodeID == c.ID && outbound.Protocol == "socks5" && outbound.FailurePolicy == "Block" {
					matched = true
				}
			}
			if !matched {
				t.Fatal("middle inlet did not use the declared chain path with Block failure handling")
			}
		}
	}
	for _, invalid := range []ChainInput{
		{Name: "Loop", NodeIDs: []string{f.nodeA, f.nodeB, f.nodeA}, OutboundIDs: outboundIDs, SupportTCP: true},
		{Name: "Too long", NodeIDs: []string{f.nodeA, f.nodeB, c.ID, "fourth"}, OutboundIDs: append(append([]string{}, outboundIDs...), "extra"), SupportTCP: true},
		{Name: "Wrong hop", NodeIDs: ids, OutboundIDs: []string{outboundIDs[1], outboundIDs[0]}, SupportTCP: true},
		{Name: "No transports", NodeIDs: ids, OutboundIDs: outboundIDs},
	} {
		if _, err := f.service.SaveChain(ctx, chain.ID, invalid); err == nil {
			t.Fatal("accepted invalid chain", invalid.Name)
		}
	}
	_, err = f.nodes.Record(ctx, c.ID, &pb.AgentMessage{Sequence: 2, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "Xray", Protocols: []*pb.ProtocolCapability{{Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ChainSnapshots(ctx, chain.ID, f.users); err == nil {
		t.Fatal("publication ignored changed UDP capability")
	}
	_, err = f.nodes.Record(ctx, c.ID, &pb.AgentMessage{Sequence: 3, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "Xray", Protocols: []*pb.ProtocolCapability{{Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true, Udp: true}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "Shared internal inlet", NodeID: f.nodeA, Protocol: "socks5", TargetInboundID: inbounds[0].ID, Enabled: true, SupportTCP: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ChainSnapshots(ctx, chain.ID, f.users); err == nil {
		t.Fatal("publication accepted a shared internal chain inlet")
	}
}

func TestChainNodeScopeIsAppliedBeforePagination(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	c, _, _, err := f.nodes.CreateEnrollment(ctx, "Other node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, chainID := range []string{"unrelated-one", "unrelated-two", "matching-one", "matching-two"} {
		nodes := []string{f.nodeA, c.ID}
		if chainID == "matching-one" || chainID == "matching-two" {
			nodes = []string{f.nodeA, f.nodeB}
		}
		if _, err := f.service.Repository.saveChain(ctx, chainID, ChainInput{Name: chainID, NodeIDs: nodes, FinalOutboundID: "direct", FailurePolicy: "Block"}, nil, true); err != nil {
			t.Fatal(err)
		}
	}
	first, total, err := f.service.Repository.Chains(ctx, f.nodeB, 1, 1)
	if err != nil || total != 2 || len(first) != 1 || first[0].ID != "matching-one" {
		t.Fatal("node scope lost matching records on first page", first, total, err)
	}
	second, total, err := f.service.Repository.Chains(ctx, f.nodeB, 2, 1)
	if err != nil || total != 2 || len(second) != 1 || second[0].ID != "matching-two" {
		t.Fatal("node scope did not paginate matching records", second, total, err)
	}
}

func TestChainSnapshotsCarryTheOwnerInboundIntent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, nodeID := range []string{f.nodeA, f.nodeB} {
		if _, err := f.nodes.Record(ctx, nodeID, &pb.AgentMessage{Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "Xray", Protocols: []*pb.ProtocolCapability{{Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true, Udp: true}}}}}); err != nil {
			t.Fatal(err)
		}
	}
	// A subscription inbound without users: the renderer disables it, but the
	// Owner enabled it, and that is the intent the batch has to carry.
	empty, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "No users yet", NodeID: f.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1081, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	internal, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "Internal", NodeID: f.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	outbound, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "Next hop", NodeID: f.nodeA, Protocol: "socks5", TargetInboundID: internal.ID, Enabled: true, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	chain, err := f.service.SaveChain(ctx, "", ChainInput{Name: "Two nodes", NodeIDs: []string{f.nodeA, f.nodeB}, OutboundIDs: []string{outbound.ID}, Enabled: true, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := f.service.ChainSnapshots(ctx, chain.ID, f.users)
	if err != nil || len(snapshots) != 2 {
		t.Fatal(err)
	}
	var target NodeSnapshot
	for _, snapshot := range snapshots {
		if snapshot.State.NodeID == f.nodeB {
			target = snapshot
		}
	}
	disabled := false
	for _, inbound := range target.State.Inbounds {
		if inbound.ID == empty.ID {
			disabled = !inbound.Enabled
		}
	}
	if !disabled {
		t.Fatal("the rendered state left a user-less inbound enabled")
	}
	for _, inboundID := range []string{empty.ID, internal.ID} {
		recorded := false
		for _, enabled := range target.InboundIntent {
			if enabled == inboundID {
				recorded = true
			}
		}
		if !recorded {
			t.Fatalf("snapshots did not record inbound %s as enabled by the owner: %v", inboundID, target.InboundIntent)
		}
	}
}

func TestBackupChainsPlanCompleteDependencyGraph(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	ids := []string{f.nodeA, f.nodeB}
	for _, name := range []string{"C", "D"} {
		n, _, _, err := f.nodes.CreateEnrollment(ctx, name, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.nodes.SetPublicIP(ctx, n.ID, "192.0.2.12"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	for _, nodeID := range ids {
		if _, err := f.nodes.Record(ctx, nodeID, &pb.AgentMessage{Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{Protocols: []*pb.ProtocolCapability{{Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true, Udp: true}}}}}); err != nil {
			t.Fatal(err)
		}
	}
	hop := func(source, target string) string {
		t.Helper()
		in, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "Internal", NodeID: target, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, InternalRelay: true, Enabled: true, DefaultOutbound: "direct"})
		if err != nil {
			t.Fatal(err)
		}
		out, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "Next", NodeID: source, Protocol: "socks5", TargetInboundID: in.ID, Enabled: true, SupportTCP: true, SupportUDP: true})
		if err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	first, second, backupHop := hop(ids[0], ids[1]), hop(ids[1], ids[2]), hop(ids[0], ids[3])
	backup, err := f.service.SaveChain(ctx, "", ChainInput{Name: "Backup", NodeIDs: []string{ids[0], ids[3]}, OutboundIDs: []string{backupHop}, Enabled: true, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := f.service.SaveChain(ctx, "", ChainInput{Name: "Primary", NodeIDs: ids[:3], OutboundIDs: []string{first, second}, Enabled: true, SupportTCP: true, SupportUDP: true, FailurePolicy: "BackupProxyChain", BackupTargetID: backup.ID})
	if err != nil {
		t.Fatal(err)
	}
	states, err := f.service.ChainSnapshots(ctx, primary.ID, f.users)
	if err != nil || len(states) != 4 {
		t.Fatal("backup graph was not included", len(states), err)
	}
	positions := map[string]int{}
	for index, snapshot := range states {
		positions[snapshot.State.NodeID] = index
	}
	if positions[ids[2]] >= positions[ids[1]] || positions[ids[1]] >= positions[ids[0]] || positions[ids[3]] >= positions[ids[0]] {
		t.Fatal("dependency order violated", positions)
	}
}

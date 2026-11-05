package proxy

import (
	"encoding/json"
	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestManagedTunnelCapabilitiesAndDerivedTarget(t *testing.T) {
	p := newPublishPanel(t)
	tunnelTestRange(t, p.canvasPanel, p.nodeA, p.nodeB, p.nodeC)
	input := tunnelTestInput(p.nodeA, p.nodeB)
	tunnel, err := p.service.SaveTunnel(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	selectTunnel := PublishSelection{TunnelIDs: []string{tunnel.ID}}
	_, err = p.service.PlanTunnels(p.ctx, selectTunnel)
	requireTunnelError(t, err, "capability")
	setCaps := func(nodeID string, version uint32) {
		t.Helper()
		caps := &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "xray", ForwardInboundSupported: true}, {Name: "gost", ForwardInboundSupported: true, GostContractVersion: version}}}
		data, _ := json.Marshal(caps)
		if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(data), nodeID); err != nil {
			t.Fatal(err)
		}
	}
	setCaps(p.nodeA, 2)
	setCaps(p.nodeB, 3)
	_, err = p.service.PlanTunnels(p.ctx, selectTunnel)
	requireTunnelError(t, err, "capability")
	setCaps(p.nodeA, 3)
	plan, err := p.service.PlanTunnels(p.ctx, selectTunnel)
	if err != nil || !plan.OK {
		t.Fatal(plan, err)
	}
	if err = (&node.Repository{Database: p.database}).SetPublicIP(p.ctx, p.nodeB, ""); err != nil {
		t.Fatal(err)
	}
	_, err = p.service.PlanTunnels(p.ctx, selectTunnel)
	requireTunnelError(t, err, "invalid")
	input = tunnel.TunnelInput
	input.Hops[1].ConnectHost = "relay.example.com"
	tunnel, err = p.service.SaveTunnel(p.ctx, tunnel.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.PlanTunnels(p.ctx, selectTunnel); err != nil {
		t.Fatal(err)
	}
	destination, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "destination", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	rule, err := p.service.SaveTunnelRule(p.ctx, "", TunnelRuleInput{Name: "linked", EntryNodeID: p.nodeA, PortMode: "manual", ListenPort: canvasTestPort(31000), Network: "tcp", TunnelID: &tunnel.ID, Target: TunnelTarget{Mode: "linked", InboundID: destination.ID}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if rule.Target.Host != "127.0.0.1" || rule.Target.Port != 1080 {
		t.Fatal(rule)
	}
	publication, err := p.service.buildTunnelPublication(p.ctx, PublishSelection{RuleIDs: []string{rule.ID}})
	if err != nil {
		t.Fatal(err)
	}
	f := publication.desired[len(publication.desired)-1].input.Settings.Forward
	if f.Host != "127.0.0.1" || f.Hops[0].Host != "relay.example.com" {
		t.Fatal(f)
	}
	input = tunnel.TunnelInput
	input.Hops = []TunnelHop{{Role: "entry", NodeID: p.nodeA, PortMode: "auto"}, {Role: "mid", NodeID: p.nodeC, PortMode: "auto"}, {Role: "exit", NodeID: p.nodeB, PortMode: "auto", ConnectHost: "relay.example.com"}}
	if _, err = p.service.SaveTunnel(p.ctx, tunnel.ID, input); err != nil {
		t.Fatal(err)
	}
	setCaps(p.nodeC, 4)
	_, err = p.service.PlanTunnels(p.ctx, selectTunnel)
	requireTunnelError(t, err, "capability")
}

func TestManagedInboundAPIAndPlanPermissions(t *testing.T) {
	p := newCanvasPanel(t)
	owner, csrf := p.session("Owner")
	viewer, vcsrf := p.session("Viewer")
	input := TunnelRuleInput{Name: "managed", EntryNodeID: p.nodeA, PortMode: "manual", ListenPort: canvasTestPort(31000), Network: "tcp", Target: TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}, Enabled: true}
	rule, err := p.service.SaveTunnelRule(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	in := managedInbound{owner: PortOwner{Kind: "rule", ID: rule.ID}, id: "generated", input: InboundInput{Name: "[隧道] managed", NodeID: p.nodeA, Protocol: "forward", Listen: "0.0.0.0", Port: 31000, Enabled: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp"}}}}
	if err = p.service.Repository.replaceManagedInbounds(p.ctx, nil, []managedInbound{in}); err != nil {
		t.Fatal(err)
	}
	assertTunnelHTTP(t, p.call("PUT", "/api/v1/inbounds/generated", in.input, owner, csrf), 409, "managed_by_tunnel")
	assertTunnelHTTP(t, p.call("DELETE", "/api/v1/inbounds/generated", nil, owner, csrf), 409, "managed_by_tunnel")
	response := p.call("GET", "/api/v1/inbounds", nil, owner, "")
	assertTunnelHTTP(t, response, 200, "")
	var body struct {
		Items []Inbound `json:"items"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, in := range body.Items {
		if in.ID == "generated" {
			found = in.ManagedBy != nil && in.ManagedBy.ID == rule.ID && in.ManagedBy.Name == rule.Name
		}
	}
	if !found {
		t.Fatal(response.Body.String())
	}
	for _, path := range []string{"/api/v1/tunnels/plan", "/api/v1/tunnels/publish"} {
		assertTunnelHTTP(t, p.call("POST", path, PublishSelection{RuleIDs: []string{rule.ID}}, viewer, vcsrf), 403, "forbidden")
		assertTunnelHTTP(t, p.call("POST", path, PublishSelection{}, owner, ""), 403, "csrf_failed")
	}
}

func TestInboundManualConflictAndAllocationModes(t *testing.T) {
	p := newCanvasPanel(t)
	r := p.service.Repository
	tunnelTestRange(t, p, p.nodeA)
	in := InboundInput{Name: "off", NodeID: p.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 30000, DefaultOutbound: "direct"}
	if _, err := r.SaveInbound(p.ctx, "", in); err != nil {
		t.Fatal(err)
	}
	check, err := r.CheckPort(p.ctx, p.nodeA, 30000, nil)
	if err != nil || check.Free || check.UsedBy.Kind != "inbound" {
		t.Fatal(check, err)
	}
	ports, err := r.AllocatePorts(p.ctx, p.nodeA, 1, nil)
	if err != nil || ports[0] != 30001 {
		t.Fatal(ports, err)
	}
	in.Name = "on"
	in.Enabled = true
	saved, err := r.SaveInbound(p.ctx, "", in)
	if err != nil {
		t.Fatal(err)
	}
	in.Name = "also on"
	if _, err = r.SaveInbound(p.ctx, "", in); err == nil {
		t.Fatal("enabled collision accepted")
	}
	in.Enabled = false
	in.Name = "another off"
	if _, err = r.SaveInbound(p.ctx, "", in); err != nil {
		t.Fatal("disabled save checked other inbounds", err)
	}
	if err = r.DeleteInbound(p.ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
}

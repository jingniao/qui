package proxy

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/desiredstate"
)

// The exit is shared by the canvas and an already published managed tunnel.
// This exercises both ownership models and their generated listeners together.
func deletionCascadeFixture(t *testing.T) (*publishPanel, string, CanvasScheme, CanvasEntryBindingRecord, Tunnel, TunnelRule) {
	t.Helper()
	p, input := quickCanvasFixture(t)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})
	layout.Instances[1].Ref = "inbound:" + input.ExitInboundID
	scheme := savePlanScheme(t, p, layout)
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, plan); err != nil {
		t.Fatal(err)
	}
	binding, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	tunnel, err := p.service.SaveTunnel(p.ctx, "", tunnelTestInput(p.nodeA, p.nodeB))
	if err != nil {
		t.Fatal(err)
	}
	rule, err := p.service.SaveTunnelRule(p.ctx, "", TunnelRuleInput{Name: "exit linked rule", EntryNodeID: p.nodeA, PortMode: "auto", Network: "tcp", TunnelID: &tunnel.ID, Target: TunnelTarget{Mode: "linked", InboundID: input.ExitInboundID}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	generated := []managedInbound{}
	for _, item := range []struct {
		kind, owner, id, node string
		port                  int
	}{
		{"rule", rule.ID, "a2-rule-listener", p.nodeA, *rule.ListenPort},
		{"tunnel", tunnel.ID, "a2-tunnel-listener", p.nodeB, *tunnel.Hops[1].ListenPort},
	} {
		generated = append(generated, managedInbound{owner: PortOwner{Kind: item.kind, ID: item.owner}, id: item.id, input: InboundInput{Name: item.id, NodeID: item.node, Protocol: "forward", Listen: "0.0.0.0", Port: item.port, Enabled: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp"}}}})
	}
	if err = p.service.Repository.replaceManagedInbounds(p.ctx, nil, generated); err != nil {
		t.Fatal(err)
	}
	markManagedPublished(t, p.canvasPanel, "tunnels", tunnel.ID)
	markManagedPublished(t, p.canvasPanel, "tunnel_rules", rule.ID)
	return p, input.ExitInboundID, scheme, binding, tunnel, rule
}

func deletionCount(t *testing.T, p *publishPanel, table, condition string, args ...any) int {
	t.Helper()
	var count int
	if err := p.database.QueryRow("SELECT count(*) FROM "+table+" WHERE "+condition, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestInboundDeletionCascadesPublishedSchemeRuleAndEmptyTunnel(t *testing.T) {
	p, exitID, scheme, binding, tunnel, rule := deletionCascadeFixture(t)
	if _, err := p.database.Exec("UPDATE nodes SET status='offline'"); err != nil {
		t.Fatal(err)
	}
	preview, err := p.service.PreviewInboundDeletion(p.ctx, exitID)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Schemes) != 1 || preview.Schemes[0].ID != scheme.ID || len(preview.Tunnels) != 1 || preview.Tunnels[0].ID != tunnel.ID || len(preview.Rules) != 1 || preview.Rules[0].ID != rule.ID || len(preview.Blockers) != 0 {
		t.Fatalf("preview %+v", preview)
	}
	inbounds := map[string]bool{}
	for _, in := range preview.Inbounds {
		if in.Name == "" || in.NodeName == "" {
			t.Fatalf("missing inbound display fields: %+v", in)
		}
		inbounds[in.ID] = true
	}
	for _, id := range []string{binding.InboundID, "a2-rule-listener", "a2-tunnel-listener"} {
		if !inbounds[id] {
			t.Fatalf("preview omitted %s: %+v", id, preview)
		}
	}
	if inbounds[exitID] {
		t.Fatal("inbound preview must list other deleted listeners, not the requested inbound")
	}
	result, err := p.service.DeleteInbound(p.ctx, exitID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PendingNodes) != 2 {
		t.Fatalf("offline affected nodes %+v", result.PendingNodes)
	}
	for _, item := range []struct {
		table, condition string
		args             []any
	}{
		{"canvas_schemes", "id=?", []any{scheme.ID}},
		{"canvas_entry_bindings", "inbound_id=?", []any{binding.InboundID}},
		{"tunnel_rules", "id=?", []any{rule.ID}},
		{"tunnels", "id=?", []any{tunnel.ID}},
		{"tunnel_objects", "owner_id IN (?,?)", []any{rule.ID, tunnel.ID}},
		{"port_claims", "owner_id IN (?,?)", []any{rule.ID, tunnel.ID}},
		{"inbounds", "id IN (?,?,?,?)", []any{exitID, binding.InboundID, "a2-rule-listener", "a2-tunnel-listener"}},
	} {
		if count := deletionCount(t, p, item.table, item.condition, item.args...); count != 0 {
			t.Fatalf("%s retained %d deleted rows", item.table, count)
		}
	}
	if count := deletionCount(t, p, "canvas_tunnel_objects", "1=1"); count != 0 {
		t.Fatalf("retained canvas objects %d", count)
	}
	if count := deletionCount(t, p, "relay_cleanup_nodes", "1=1"); count != 2 {
		t.Fatalf("cleanup work %d", count)
	}
}

func TestInboundDeletionEntryRemovesWholeScheme(t *testing.T) {
	p, scheme, binding := cleanupFixture(t)
	preview, err := p.service.PreviewInboundDeletion(p.ctx, binding.InboundID)
	if err != nil || len(preview.Schemes) != 1 || preview.Schemes[0].ID != scheme.ID || len(preview.Blockers) != 0 {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = p.service.DeleteInbound(p.ctx, binding.InboundID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"canvas_schemes", "canvas_tunnels", "canvas_tunnel_objects", "canvas_entry_bindings"} {
		if count := deletionCount(t, p, table, "1=1"); count != 0 {
			t.Fatalf("%s rows %d", table, count)
		}
	}
	if _, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("entry survived: %v", err)
	}
}

func TestInboundDeletionManualRelayBlockerHTTP(t *testing.T) {
	p := newCanvasPanel(t)
	outbound, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "manual relay", NodeID: p.nodeA, Protocol: "socks5", Enabled: true, TargetInboundID: p.inbound.ID, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	owner, csrf := p.session("Owner")
	response := p.call("GET", "/api/v1/inbounds/"+p.inbound.ID+"/deletion-preview", nil, owner, "")
	assertTunnelHTTP(t, response, 200, "")
	var preview node.DeletionImpact
	if err = json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Blockers) != 1 || preview.Blockers[0].Kind != "outbound" || preview.Blockers[0].ID != outbound.ID || preview.Blockers[0].Name != outbound.Name {
		t.Fatalf("blockers %+v", preview.Blockers)
	}
	response = p.call("DELETE", "/api/v1/inbounds/"+p.inbound.ID, nil, owner, csrf)
	assertTunnelHTTP(t, response, 409, "inbound_delete_blocked")
	// HTTP error fields become APIError.details in the shared API client.
	for _, value := range []string{`"blockers"`, `"kind":"outbound"`, outbound.ID, outbound.Name} {
		if !strings.Contains(response.Body.String(), value) {
			t.Fatalf("missing %s in %s", value, response.Body.String())
		}
	}
	if _, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, false); err != nil {
		t.Fatal(err)
	}
}

func TestInboundDeletionHTTPContract(t *testing.T) {
	p, exitID, _, _, _, _ := deletionCascadeFixture(t)
	owner, csrf := p.session("Owner")
	path := "/api/v1/inbounds/" + exitID
	response := p.call("GET", path+"/deletion-preview", nil, owner, "")
	assertTunnelHTTP(t, response, 200, "")
	var preview map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schemes", "tunnels", "rules", "inbounds", "blockers"} {
		if _, ok := preview[key]; !ok || string(preview[key]) == "null" {
			t.Fatalf("missing array %s: %s", key, response.Body.String())
		}
	}
	response = p.call("DELETE", path, nil, owner, csrf)
	assertTunnelHTTP(t, response, 200, "")
	var result map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if raw, ok := result["pendingNodes"]; !ok || string(raw) == "null" {
		t.Fatalf("missing pendingNodes array: %s", response.Body.String())
	}
	assertTunnelHTTP(t, p.call("GET", path+"/deletion-preview", nil, owner, ""), 404, "not_found")
}

func TestInboundDeletionCascadeRollsBackAllObjectsAndCleanup(t *testing.T) {
	p, exitID, scheme, binding, tunnel, rule := deletionCascadeFixture(t)
	if _, err := p.database.Exec("CREATE TRIGGER reject_a2_cleanup BEFORE DELETE ON inbounds WHEN OLD.id='a2-rule-listener' BEGIN SELECT RAISE(ABORT,'a2 cleanup rejected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.DeleteInbound(p.ctx, exitID); err == nil || !strings.Contains(err.Error(), "a2 cleanup rejected") {
		t.Fatalf("delete error %v", err)
	}
	for _, item := range []struct{ table, id string }{{"canvas_schemes", scheme.ID}, {"tunnel_rules", rule.ID}, {"tunnels", tunnel.ID}, {"inbounds", exitID}, {"inbounds", binding.InboundID}, {"inbounds", "a2-rule-listener"}, {"inbounds", "a2-tunnel-listener"}} {
		if count := deletionCount(t, p, item.table, "id=?", item.id); count != 1 {
			t.Fatalf("partial delete %s %s", item.table, item.id)
		}
	}
	if count := deletionCount(t, p, "relay_cleanup_nodes", "1=1"); count != 0 {
		t.Fatalf("transaction leaked cleanup rows %d", count)
	}
}

func TestNodeDeletionCascadesRelayAndClearsOwnCleanup(t *testing.T) {
	p, exitID, scheme, binding, tunnel, rule := deletionCascadeFixture(t)
	nodes := p.service.Nodes.(*node.Repository)
	nodes.RelayDeletion = p.service
	if _, err := p.database.Exec("UPDATE nodes SET status='offline'"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("INSERT INTO relay_cleanup_nodes(node_id,reason,next_attempt_at,created_at,updated_at) VALUES(?,'older cleanup',?,?,?)", p.nodeB, timestamp(), timestamp(), timestamp()); err != nil {
		t.Fatal(err)
	}
	preview, err := nodes.Preview(p.ctx, p.nodeB)
	if err != nil || len(preview.Schemes) != 1 || len(preview.Rules) != 1 || len(preview.Tunnels) != 1 || len(preview.Inbounds) < 3 || len(preview.Blockers) != 0 {
		t.Fatalf("preview %+v %v", preview, err)
	}
	result, err := nodes.DeleteWithResult(p.ctx, p.nodeB, nil, nil, "owner", "ip")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ table, id string }{{"canvas_schemes", scheme.ID}, {"tunnels", tunnel.ID}, {"tunnel_rules", rule.ID}, {"inbounds", exitID}, {"inbounds", binding.InboundID}} {
		if count := deletionCount(t, p, item.table, "id=?", item.id); count != 0 {
			t.Fatalf("retained %s %s", item.table, item.id)
		}
	}
	if count := deletionCount(t, p, "relay_cleanup_nodes", "node_id=?", p.nodeB); count != 0 {
		t.Fatal("deleted server retained cleanup row")
	}
	if len(result.PendingNodes) != 1 || result.PendingNodes[0].NodeID != p.nodeA {
		t.Fatalf("pending nodes %+v", result.PendingNodes)
	}
	deleted, err := nodes.Get(p.ctx, p.nodeB)
	if err != nil || deleted.DeletedAt == "" {
		t.Fatalf("node %+v %v", deleted, err)
	}
}

func TestInboundDeletionPreservesTunnelWithSurvivingRule(t *testing.T) {
	p, exitID, _, _, tunnel, deletedRule := deletionCascadeFixture(t)
	survivor, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "surviving destination", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1081, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	survivingRule, err := p.service.SaveTunnelRule(p.ctx, "", TunnelRuleInput{Name: "surviving rule", EntryNodeID: p.nodeA, PortMode: "auto", Network: "tcp", TunnelID: &tunnel.ID, Target: TunnelTarget{Mode: "linked", InboundID: survivor.ID}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := p.service.PreviewInboundDeletion(p.ctx, exitID)
	if err != nil || len(preview.Tunnels) != 0 || len(preview.Rules) != 1 || preview.Rules[0].ID != deletedRule.ID {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = p.service.DeleteInbound(p.ctx, exitID); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ table, id string }{{"tunnels", tunnel.ID}, {"tunnel_rules", survivingRule.ID}, {"inbounds", survivor.ID}, {"inbounds", "a2-tunnel-listener"}} {
		if count := deletionCount(t, p, item.table, "id=?", item.id); count != 1 {
			t.Fatalf("shared tunnel lost %s %s", item.table, item.id)
		}
	}
	if count := deletionCount(t, p, "port_claims", "owner_id=?", tunnel.ID); count == 0 {
		t.Fatal("shared tunnel lost port claims")
	}
}

func TestInboundDeletionManagedListenerStillRefused(t *testing.T) {
	p, _, _, _, _, _ := deletionCascadeFixture(t)
	owner, csrf := p.session("Owner")
	for _, id := range []string{"a2-rule-listener", "a2-tunnel-listener"} {
		assertTunnelHTTP(t, p.call("DELETE", "/api/v1/inbounds/"+id, nil, owner, csrf), 409, "managed_by_tunnel")
		if count := deletionCount(t, p, "inbounds", "id=?", id); count != 1 {
			t.Fatalf("managed listener deleted: %s", id)
		}
	}
}

func TestNodeDeletionMiddleTunnelHopCascadesWithoutDeletingExit(t *testing.T) {
	p, exitID, scheme, _, tunnel, rule := deletionCascadeFixture(t)
	input := tunnel.TunnelInput
	input.Hops = []TunnelHop{{Role: "entry", NodeID: p.nodeA, PortMode: "auto"}, {Role: "mid", NodeID: p.nodeC, PortMode: "auto"}, {Role: "exit", NodeID: p.nodeB, PortMode: "auto"}}
	if _, err := p.service.SaveTunnel(p.ctx, tunnel.ID, input); err != nil {
		t.Fatal(err)
	}
	nodes := p.service.Nodes.(*node.Repository)
	nodes.RelayDeletion = p.service
	preview, err := nodes.Preview(p.ctx, p.nodeC)
	if err != nil || len(preview.Tunnels) != 1 || preview.Tunnels[0].ID != tunnel.ID || len(preview.Rules) != 1 || preview.Rules[0].ID != rule.ID || len(preview.Schemes) != 0 || len(preview.Blockers) != 0 {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = nodes.DeleteWithResult(p.ctx, p.nodeC, nil, nil, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ table, id string }{{"tunnels", tunnel.ID}, {"tunnel_rules", rule.ID}, {"inbounds", "a2-rule-listener"}, {"inbounds", "a2-tunnel-listener"}} {
		if count := deletionCount(t, p, item.table, "id=?", item.id); count != 0 {
			t.Fatalf("middle hop retained %s %s", item.table, item.id)
		}
	}
	for _, item := range []struct{ table, id string }{{"canvas_schemes", scheme.ID}, {"inbounds", exitID}} {
		if count := deletionCount(t, p, item.table, "id=?", item.id); count != 1 {
			t.Fatalf("middle hop deleted unrelated %s %s", item.table, item.id)
		}
	}
}

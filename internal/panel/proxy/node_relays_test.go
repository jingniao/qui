package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	subproxy "proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source/published"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestNodeRelayHandlersAndInboundDeletion(t *testing.T) {
	p := newCanvasPanel(t)
	tunnelTestRange(t, p, p.nodeA, p.nodeB)
	owner, csrf := p.session("Owner")
	viewer, viewerCSRF := p.session("Viewer")
	tunnel, err := p.service.SaveTunnel(p.ctx, "", tunnelTestInput(p.nodeA, p.nodeB))
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := p.service.Repository.SaveInbound(p.ctx, "", InboundInput{Name: "user", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1081, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/nodes/" + p.nodeB + "/relays"
	input := NodeRelayInput{TunnelID: tunnel.ID, InboundID: inbound.ID, PortMode: "auto", SubscriptionVisible: true}
	assertTunnelHTTP(t, p.call("GET", path, nil, nil, ""), 401, "unauthorized")
	assertTunnelHTTP(t, p.call("GET", "/api/v1/nodes/missing/relays", nil, viewer, ""), 404, "not_found")
	assertTunnelHTTP(t, p.call("POST", "/api/v1/nodes/missing/relays", input, owner, csrf), 404, "not_found")
	assertTunnelHTTP(t, p.call("POST", path, input, viewer, viewerCSRF), 403, "forbidden")
	assertTunnelHTTP(t, p.call("POST", path, input, owner, ""), 403, "csrf_failed")
	wrong := input
	wrong.InboundID = p.inbound.ID
	assertTunnelHTTP(t, p.call("POST", path, wrong, owner, csrf), 400, "invalid")
	assertTunnelHTTP(t, p.call("POST", "/api/v1/nodes/"+p.nodeA+"/relays", input, owner, csrf), 400, "invalid")
	wrong = input
	wrong.TunnelID = "missing"
	assertTunnelHTTP(t, p.call("POST", path, wrong, owner, csrf), 404, "not_found")
	wrong = input
	wrong.PortMode = "invalid"
	assertTunnelHTTP(t, p.call("POST", path, wrong, owner, csrf), 400, "invalid")
	wrong = input
	wrong.PortMode, wrong.ListenPort = "manual", canvasTestPort(0)
	assertTunnelHTTP(t, p.call("POST", path, wrong, owner, csrf), 400, "port_out_of_range")
	response := p.call("POST", path, input, owner, csrf)
	assertTunnelHTTP(t, response, 201, "")
	var item NodeRelay
	if err = json.Unmarshal(response.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Rule.OwnerNodeID == nil || *item.Rule.OwnerNodeID != p.nodeB || item.Rule.Name != "Node B 经 A to B" || item.Rule.Network != "both" || item.ShareLink != nil {
		t.Fatal(item)
	}
	response = p.call("GET", path, nil, viewer, "")
	assertTunnelHTTP(t, response, 200, "")
	if !strings.Contains(response.Body.String(), `"shareLink":null`) || !strings.Contains(response.Body.String(), item.Rule.ID) {
		t.Fatal(response.Body.String())
	}
	response = p.call("DELETE", "/api/v1/inbounds/"+inbound.ID, nil, owner, csrf)
	assertTunnelHTTP(t, response, 409, "inbound_has_relays")
	if !strings.Contains(response.Body.String(), item.Rule.ID) {
		t.Fatal(response.Body.String())
	}
	if err = p.service.DeleteTunnelRule(p.ctx, item.Rule.ID); err != nil {
		t.Fatal(err)
	}
	assertTunnelHTTP(t, p.call("DELETE", "/api/v1/inbounds/"+inbound.ID, nil, owner, csrf), 204, "")
	if len(p.audit.actions) < 1 {
		t.Fatal("missing audit")
	}
	if err = p.service.DeleteTunnel(p.ctx, tunnel.ID); err != nil {
		t.Fatal(err)
	}
	// Published removals remain in the library but cannot accept new rules.
	tunnel, err = p.service.SaveTunnel(p.ctx, "", tunnelTestInput(p.nodeA, p.nodeB))
	if err != nil {
		t.Fatal(err)
	}
	markManagedPublished(t, p, "tunnels", tunnel.ID)
	if err = p.service.DeleteTunnel(p.ctx, tunnel.ID); err != nil {
		t.Fatal(err)
	}
	input.TunnelID = tunnel.ID
	assertTunnelHTTP(t, p.call("POST", path, input, owner, csrf), 409, "pending_delete")
}

type relayReceipt struct {
	canvasDeployRecorder
	apply func() error
}

func (r *relayReceipt) PublishTunnels(_ context.Context, _, _ string, snapshots []NodeSnapshot) (CanvasPublishResult, error) {
	status := "applied"
	if err := r.apply(); err != nil {
		status = "failed"
	}
	result := CanvasPublishResult{}
	for _, snapshot := range snapshots {
		result.Nodes = append(result.Nodes, CanvasPublishNode{NodeID: snapshot.State.NodeID, Status: status, Revision: 1})
	}
	return result, nil
}

func TestRelaySnapshotCapturedBeforeApplyAndRetainedOnFailure(t *testing.T) {
	p := newPublishPanel(t)
	caps, _ := json.Marshal(&pb.Capabilities{CoreName: "xray", ForwardInboundSupported: true})
	if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(caps), p.nodeB); err != nil {
		t.Fatal(err)
	}
	rule, err := p.service.SaveTunnelRule(p.ctx, "", TunnelRuleInput{Name: "relay", EntryNodeID: p.nodeB, ListenPort: canvasTestPort(31000), PortMode: "manual", Network: "tcp", Target: TunnelTarget{Mode: "linked", InboundID: p.inbound.ID}, Enabled: true, SubscriptionVisible: true})
	if err != nil {
		t.Fatal(err)
	}
	receipt := &relayReceipt{apply: func() error {
		return (&node.Repository{Database: p.database}).SetPublicIP(p.ctx, p.nodeB, "203.0.113.9")
	}}
	p.service.Deploy = receipt
	result, err := p.service.PublishTunnels(p.ctx, PublishSelection{RuleIDs: []string{rule.ID}}, "owner", p.users)
	if err != nil || len(result.Servers) != 1 || result.Servers[0].Status != "applied" {
		t.Fatal(result, err)
	}
	store := &published.RelayStore{Database: p.database}
	before, err := store.Relays(p.ctx)
	if err != nil || len(before) != 1 || before[0].Server != "192.0.2.11" {
		t.Fatal(before, err)
	}
	rule, err = p.service.Repository.TunnelRule(p.ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := rule.TunnelRuleInput
	input.ListenPort = canvasTestPort(31001)
	if _, err = p.service.SaveTunnelRule(p.ctx, rule.ID, input); err != nil {
		t.Fatal(err)
	}
	receipt.apply = func() error { return errors.New("apply failed") }
	result, err = p.service.PublishTunnels(p.ctx, PublishSelection{RuleIDs: []string{rule.ID}}, "owner", p.users)
	if err != nil || len(result.Servers) != 1 || result.Servers[0].Status != "failed" {
		t.Fatal(result, err)
	}
	after, err := store.Relays(p.ctx)
	if err != nil || len(after) != 1 || after[0] != before[0] {
		t.Fatal(after, err)
	}
}

func TestRelaySnapshotKeepsPublishedAddressesUntilRepublish(t *testing.T) {
	p := newCanvasPanel(t)
	r := p.service.Repository
	inbound, err := r.SaveInbound(p.ctx, "", InboundInput{Name: "user", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1081, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	rule, err := p.service.SaveTunnelRule(p.ctx, "", TunnelRuleInput{Name: "relay", EntryNodeID: p.nodeA, ListenPort: canvasTestPort(31000), PortMode: "manual", Network: "both", Target: TunnelTarget{Mode: "linked", InboundID: inbound.ID}, Enabled: true, SubscriptionVisible: true})
	if err != nil {
		t.Fatal(err)
	}
	store := &published.RelayStore{Database: p.database}
	items, err := store.Relays(p.ctx)
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	finish := func(rule TunnelRule) {
		t.Helper()
		relay, e := r.relaySnapshot(p.ctx, p.database, rule)
		if e != nil {
			t.Fatal(e)
		}
		if e := r.finishTunnelPublication(p.ctx, tunnelPublication{rules: []TunnelRule{rule}, relays: map[string]published.Relay{rule.ID: relay}}); e != nil {
			t.Fatal(e)
		}
	}
	finish(rule)
	before, err := store.Relays(p.ctx)
	if err != nil || len(before) != 1 {
		t.Fatal(before, err)
	}
	nodes := &node.Repository{Database: p.database}
	if err = nodes.SetPublicIP(p.ctx, p.nodeA, "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	if err = nodes.SetPublicIP(p.ctx, p.nodeB, "203.0.113.10"); err != nil {
		t.Fatal(err)
	}
	input := InboundInput{Name: inbound.Name, NodeID: inbound.NodeID, Protocol: inbound.Protocol, Listen: inbound.Listen, Port: inbound.Port, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"}
	input.Port = 1082
	if _, err = r.SaveInbound(p.ctx, inbound.ID, input); err != nil {
		t.Fatal(err)
	}
	draft, err := r.TunnelRule(p.ctx, rule.ID)
	if err != nil || draft.Status != "needs_publish" {
		t.Fatal(draft, err)
	}
	after, err := store.Relays(p.ctx)
	if err != nil || len(after) != 1 || after[0] != before[0] {
		t.Fatal(after, before, err)
	}
	endpoint, ok := published.RelayEndpoint(subproxy.Proxy{Name: "destination · vless", Protocol: "vless", TLS: &subproxy.TLS{}, Server: "203.0.113.10", Port: 1082}, after[0])
	if !ok || endpoint.Server != "192.0.2.10" || endpoint.Port != 31000 || endpoint.TLS.ServerName != "192.0.2.11" {
		t.Fatal(endpoint)
	}
	finish(draft)
	after, err = store.Relays(p.ctx)
	if err != nil || after[0].Server != "203.0.113.9" || after[0].DestinationAddress != "203.0.113.10" || after[0].DestinationPort != 1082 {
		t.Fatal(after, err)
	}
	if err = p.service.DeleteTunnelRule(p.ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := r.TunnelRule(p.ctx, rule.ID)
	if err != nil || pending.Status != "pending_delete" {
		t.Fatal(pending, err)
	}
	listed, err := r.NodeRelayRules(p.ctx, p.nodeB)
	if err != nil || len(listed) != 1 || listed[0].Status != "pending_delete" {
		t.Fatal(listed, err)
	}
	remaining, err := store.Relays(p.ctx)
	if err != nil || len(remaining) != 1 || remaining[0] != after[0] {
		t.Fatal(remaining, err)
	}
	if err = r.finishTunnelPublication(p.ctx, tunnelPublication{rules: []TunnelRule{pending}}); err != nil {
		t.Fatal(err)
	}
	remaining, err = store.Relays(p.ctx)
	if err != nil || len(remaining) != 0 {
		t.Fatal(remaining, err)
	}
}

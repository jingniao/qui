package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	subproxy "proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source/published"
	pb "proxycontrol/pkg/contracts/agentpb"
)

type failingRelayShareSource struct{ calls int }

func (s *failingRelayShareSource) Proxies(context.Context, string, string) ([]subproxy.Proxy, []subproxy.Warning, error) {
	s.calls++
	return nil, nil, errors.New("share generation unavailable")
}

func TestNodeRelayBatchDisplaysRulesWithoutShares(t *testing.T) {
	p := newCanvasPanel(t)
	tunnelTestRange(t, p, p.nodeA, p.nodeB)
	owner, _ := p.session("Owner")
	viewer, _ := p.session("Viewer")
	source := &failingRelayShareSource{}
	p.handler.RelayProxies = source
	if _, err := p.database.Exec("INSERT INTO users(id,username,display_name,status,max_devices,max_connections,protocols_json,created_at) VALUES('share-user','share-user','Share user','active',1,1,'[]','now')"); err != nil {
		t.Fatal(err)
	}
	tunnel, err := p.service.SaveTunnel(p.ctx, "", tunnelTestInput(p.nodeA, p.nodeB))
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "user", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1081, Enabled: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.CreateNodeRelay(p.ctx, p.nodeB, NodeRelayInput{TunnelID: tunnel.ID, InboundID: inbound.ID, PortMode: "auto"}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,target_mode,subscription_visible,enabled,created_at,updated_at,status) VALUES('missing-import','Imported relay',?,31000,'manual','both','import',1,1,'now','now','pending_delete')", p.nodeA); err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: p.database}
	empty, _, _, err := nodes.CreateEnrollment(p.ctx, "Empty", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	deleted, _, _, err := nodes.CreateEnrollment(p.ctx, "Deleted", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("UPDATE nodes SET deleted_at='now' WHERE id=?", deleted.ID); err != nil {
		t.Fatal(err)
	}

	type entry struct {
		NodeID string
		Rules  []TunnelRule
		Error  *struct{ Code string }
	}
	ids := []string{p.nodeA, p.nodeB, empty.ID, "missing", deleted.ID}
	path := "/api/v1/nodes/relay-rules?nodeIds=" + strings.Join(ids, ",")
	assertTunnelHTTP(t, p.call("GET", path, nil, nil, ""), 401, "unauthorized")
	legacyPath := "/api/v1/nodes/" + p.nodeB + "/relays"
	assertTunnelHTTP(t, p.call("GET", legacyPath, nil, owner, ""), 500, "internal_error")
	if source.calls != 1 {
		t.Fatalf("legacy route called share generation %d times", source.calls)
	}
	first := p.call("GET", path, nil, owner, "")
	assertTunnelHTTP(t, first, 200, "")
	second := p.call("GET", path, nil, viewer, "")
	assertTunnelHTTP(t, second, 200, "")
	if source.calls != 1 || strings.Contains(first.Body.String(), "shareLink") || second.Body.String() != first.Body.String() {
		t.Fatalf("share calls=%d, Owner=%s, Viewer=%s", source.calls, first.Body.String(), second.Body.String())
	}
	var result struct{ Items []entry }
	if err = json.Unmarshal(first.Body.Bytes(), &result); err != nil || len(result.Items) != len(ids) {
		t.Fatal(first.Body.String(), err)
	}
	for i, item := range result.Items {
		if item.NodeID != ids[i] {
			t.Fatal(item.NodeID, ids[i])
		}
		if i >= 3 {
			if item.Error == nil || item.Error.Code != "not_found" || len(item.Rules) != 0 {
				t.Fatal(first.Body.String())
			}
			continue
		}
		response := p.call("GET", "/api/v1/nodes/"+item.NodeID+"/relays", nil, viewer, "")
		assertTunnelHTTP(t, response, 200, "")
		var legacy struct{ Items []NodeRelay }
		if err = json.Unmarshal(response.Body.Bytes(), &legacy); err != nil {
			t.Fatal(response.Body.String(), err)
		}
		want := make([]TunnelRule, 0, len(legacy.Items))
		for _, relay := range legacy.Items {
			want = append(want, relay.Rule)
		}
		if item.Error != nil || !reflect.DeepEqual(item.Rules, want) {
			t.Fatalf("node %s: rules=%+v want=%+v error=%+v", item.NodeID, item.Rules, want, item.Error)
		}
	}

	// One invalid target makes its old list fail, but must not hide another node's rules.
	if _, err = p.database.Exec("UPDATE inbounds SET internal_relay=1 WHERE id=?", inbound.ID); err != nil {
		t.Fatal(err)
	}
	broken := p.call("GET", path, nil, viewer, "")
	assertTunnelHTTP(t, broken, 200, "")
	result.Items = nil
	if err = json.Unmarshal(broken.Body.Bytes(), &result); err != nil || len(result.Items) != len(ids) {
		t.Fatal(broken.Body.String(), err)
	}
	if len(result.Items[0].Rules) != 1 || result.Items[0].Error != nil || len(result.Items[1].Rules) != 0 || result.Items[1].Error == nil || result.Items[1].Error.Code != "invalid" {
		t.Fatal(broken.Body.String())
	}
	for _, malformed := range []string{"", p.nodeA + "," + p.nodeA, p.nodeA + ","} {
		assertTunnelHTTP(t, p.call("GET", "/api/v1/nodes/relay-rules?nodeIds="+malformed, nil, owner, ""), 400, "invalid_request")
	}
}

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
	assertTunnelHTTP(t, response, 200, "")
	if !strings.Contains(response.Body.String(), `"pendingNodes":`) {
		t.Fatal(response.Body.String())
	}
	if _, err = p.service.Repository.TunnelRule(p.ctx, item.Rule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("linked rule survived inbound deletion: %v", err)
	}
	if _, err = p.service.Repository.Tunnel(p.ctx, tunnel.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty tunnel survived inbound deletion: %v", err)
	}
	if len(p.audit.actions) < 1 {
		t.Fatal("missing audit")
	}
	// Published removals remain in the library but cannot accept new rules.
	inbound, err = p.service.Repository.SaveInbound(p.ctx, "", InboundInput{Name: "replacement user", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1081, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	input.InboundID = inbound.ID
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
	apply    func() error
	database *sql.DB
	result   CanvasPublishResult
}

var _ TunnelDeployer = (*relayReceipt)(nil)

func (r *relayReceipt) CheckTunnelPublicationNodes(context.Context, *sql.Tx, []string) error {
	return nil
}

func (r *relayReceipt) TunnelPublicationResult(context.Context, string) (CanvasPublishResult, error) {
	return r.result, nil
}

func (r *relayReceipt) GostRemovable(context.Context, string) (bool, error) {
	return false, nil
}

func (r *relayReceipt) PublishTunnels(ctx context.Context, batchID, _, _ string, snapshots []NodeSnapshot) (CanvasPublishResult, error) {
	status := "applied"
	batchStatus := "success"
	if err := r.apply(); err != nil {
		status = "failed"
		batchStatus = "failed"
	}
	result := CanvasPublishResult{BatchID: batchID}
	for _, snapshot := range snapshots {
		result.Nodes = append(result.Nodes, CanvasPublishNode{NodeID: snapshot.State.NodeID, Status: status, Revision: 1})
	}
	r.result = result
	_, err := r.database.ExecContext(ctx, "UPDATE deployment_batches SET status=? WHERE id=?", batchStatus, batchID)
	return result, err
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
	receipt := &relayReceipt{database: p.database, apply: func() error {
		return (&node.Repository{Database: p.database}).SetPublicIP(p.ctx, p.nodeB, "203.0.113.9")
	}}
	p.service.Deploy = receipt
	result, err := p.service.PublishTunnels(p.ctx, PublishSelection{RuleIDs: []string{rule.ID}}, "owner", p.users)
	if err != nil || len(result.Servers) != 1 || result.Servers[0].Status != "applied" {
		t.Fatal(result, err)
	}
	store := &published.RelayStore{Database: p.database}
	before, err := store.Relays(p.ctx)
	if err != nil || len(before) != 1 || before[0].Server != "203.0.113.9" {
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
	if err != nil || len(after) != 1 || !reflect.DeepEqual(after[0], before[0]) {
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
	// The entry dial address follows the entry node live (it is not runtime
	// configuration); the destination address and port stay frozen.
	want := before[0]
	want.Server = "203.0.113.9"
	if err != nil || len(after) != 1 || !reflect.DeepEqual(after[0], want) {
		t.Fatal(after, before, err)
	}
	endpoint, ok := published.RelayEndpoint(subproxy.Proxy{Name: "destination · vless", Protocol: "vless", TLS: &subproxy.TLS{}, Server: "203.0.113.10", Port: 1082}, after[0])
	if !ok || endpoint.Server != "203.0.113.9" || endpoint.Port != 31000 || endpoint.TLS.ServerName != "192.0.2.11" {
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
	if err != nil || len(remaining) != 1 || !reflect.DeepEqual(remaining[0], after[0]) {
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

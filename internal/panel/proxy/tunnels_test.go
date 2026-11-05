package proxy

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"proxycontrol/internal/panel/node"
	"strings"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

func tunnelTestInput(a, b string) TunnelInput {
	return TunnelInput{Name: "A to B", Transport: "tls", Hops: []TunnelHop{{Role: "entry", NodeID: a, PortMode: "auto"}, {Role: "exit", NodeID: b, PortMode: "auto"}}}
}
func tunnelTestRange(t *testing.T, p *canvasPanel, nodes ...string) {
	t.Helper()
	for _, node := range nodes {
		if _, err := p.database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,30000,30005)", node); err != nil {
			t.Fatal(err)
		}
	}
}
func requireTunnelError(t *testing.T, err error, code string) {
	t.Helper()
	var e *tunnelError
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func markManagedPublished(t *testing.T, p *canvasPanel, table, objectID string) {
	t.Helper()
	if _, err := p.database.Exec("UPDATE "+table+" SET status='published',published_at=updated_at,published_version=draft_version WHERE id=?", objectID); err != nil {
		t.Fatal(err)
	}
}
func TestTunnelDraftLifecycleAndClaims(t *testing.T) {
	p := newPublishPanel(t)
	r := p.service.Repository
	tunnelTestRange(t, p.canvasPanel, p.nodeA, p.nodeB, p.nodeC)
	input := tunnelTestInput(p.nodeA, p.nodeB)
	tunnel, err := p.service.SaveTunnel(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if tunnel.Status != "draft" || tunnel.PublishedAt != nil || len(tunnel.Hops) != 2 || tunnel.Hops[0].ListenPort != nil || *tunnel.Hops[1].ListenPort != 30000 || tunnel.Credentials == nil || tunnel.Credentials.Generation != 1 {
		t.Fatal(tunnel)
	}
	if string(tunnel.sealedMaterial) == "" || strings.Contains(string(tunnel.sealedMaterial), "PRIVATE KEY") {
		t.Fatal("unsealed material")
	}
	data, _ := json.Marshal(tunnel)
	if strings.Contains(string(data), "PRIVATE KEY") || strings.Contains(string(data), "gost_material") {
		t.Fatal("material exposed")
	}
	input = tunnel.TunnelInput
	input.Hops[1].ListenPort = nil
	edited, err := p.service.SaveTunnel(p.ctx, tunnel.ID, input)
	if err != nil || *edited.Hops[1].ListenPort != 30000 {
		t.Fatal(edited, err)
	}
	_, err = p.service.SaveTunnel(p.ctx, tunnel.ID, input)
	requireTunnelError(t, err, "stale")
	markManagedPublished(t, p.canvasPanel, "tunnels", tunnel.ID)
	input = edited.TunnelInput
	input.Hops[1].ListenPort = canvasTestPort(30002)
	edited, err = p.service.SaveTunnel(p.ctx, tunnel.ID, input)
	if err != nil || edited.Status != "modified" {
		t.Fatal(edited, err)
	}
	for _, port := range []int{30000, 30002} {
		check, e := r.CheckPort(p.ctx, p.nodeB, port, nil)
		if e != nil || check.Free || check.UsedBy.Kind != "tunnel" || check.UsedBy.ID != tunnel.ID {
			t.Fatal(check, e)
		}
	}
	rotated, err := p.service.RotateTunnelCredentials(p.ctx, tunnel.ID, edited.UpdatedAt)
	if err != nil || rotated.Credentials.Generation != 2 || rotated.Credentials.Fingerprint == edited.Credentials.Fingerprint || rotated.Credentials.ServerName != edited.Credentials.ServerName {
		t.Fatal(rotated, err)
	}
	if err = p.service.DeleteTunnel(p.ctx, tunnel.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := r.Tunnel(p.ctx, tunnel.ID)
	if err != nil || pending.Status != "pending_delete" {
		t.Fatal(pending, err)
	}
	_, err = p.service.SaveTunnel(p.ctx, tunnel.ID, pending.TunnelInput)
	requireTunnelError(t, err, "pending_delete")
	_, err = p.service.RotateTunnelCredentials(p.ctx, tunnel.ID, pending.UpdatedAt)
	requireTunnelError(t, err, "pending_delete")
	check, err := r.CheckPort(p.ctx, p.nodeB, 30000, nil)
	if err != nil || check.Free {
		t.Fatal(check, err)
	}
	input = tunnelTestInput(p.nodeA, p.nodeC)
	input.Name = "draft to delete"
	draft, err := p.service.SaveTunnel(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.service.DeleteTunnel(p.ctx, draft.ID); err != nil {
		t.Fatal(err)
	}
	check, err = r.CheckPort(p.ctx, p.nodeC, *draft.Hops[1].ListenPort, nil)
	if err != nil || !check.Free {
		t.Fatal(check, err)
	}
	if err = ValidateTunnels(p.ctx, p.database, r.Vault); err != nil {
		t.Fatal(err)
	}
}

func TestTunnelRulesFollowDestinationAndRetarget(t *testing.T) {
	p := newPublishPanel(t)
	r := p.service.Repository
	tunnelTestRange(t, p.canvasPanel, p.nodeA, p.nodeB, p.nodeC)
	input := tunnelTestInput(p.nodeA, p.nodeB)
	tunnel, err := p.service.SaveTunnel(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	target, err := r.SaveInbound(p.ctx, "", InboundInput{Name: "destination", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	ruleInput := TunnelRuleInput{Name: "linked", EntryNodeID: p.nodeA, PortMode: "auto", Network: "both", TunnelID: &tunnel.ID, Target: TunnelTarget{Mode: "linked", InboundID: target.ID}, SubscriptionVisible: true, Enabled: true}
	rule, err := p.service.SaveTunnelRule(p.ctx, "", ruleInput)
	if err != nil || rule.Target.Host != "127.0.0.1" || rule.Target.Port != 1080 || rule.Target.NodeName == "" || rule.EntryNodeName == "" || rule.TunnelName == nil || *rule.ListenPort != 30000 {
		t.Fatal(rule, err)
	}
	tunnel, err = r.Tunnel(p.ctx, tunnel.ID)
	if err != nil || tunnel.RuleCount != 1 {
		t.Fatal(tunnel, err)
	}
	err = p.service.DeleteTunnel(p.ctx, tunnel.ID)
	requireTunnelError(t, err, "tunnel_in_use")
	markManagedPublished(t, p.canvasPanel, "tunnel_rules", rule.ID)
	if err = (&node.Repository{Database: p.database}).SetPublicIP(p.ctx, p.nodeB, "203.0.113.20"); err != nil {
		t.Fatal(err)
	}
	changed, err := r.TunnelRule(p.ctx, rule.ID)
	if err != nil || changed.Status != "needs_publish" || changed.UpdatedAt == rule.UpdatedAt {
		t.Fatal(changed, err)
	}
	oldStamp := changed.UpdatedAt
	targetInput := inboundInputOf(target, target.DefaultOutbound)
	targetInput.Port = 1081
	if _, err = r.SaveInbound(p.ctx, target.ID, targetInput); err != nil {
		t.Fatal(err)
	}
	changed, err = r.TunnelRule(p.ctx, rule.ID)
	if err != nil || changed.Target.Port != 1081 || changed.UpdatedAt == oldStamp {
		t.Fatal(changed, err)
	}
	// A tunnel edit must advance each rule's own token, including when the rule
	// was edited independently after the last tunnel save.
	synchronizedStamp := nextCanvasStamp(tunnel.UpdatedAt)
	if _, err = p.database.Exec("UPDATE tunnel_rules SET updated_at=? WHERE id=?", synchronizedStamp, rule.ID); err != nil {
		t.Fatal(err)
	}
	input = tunnel.TunnelInput
	input.Hops[0].NodeID = p.nodeC
	input.Hops[1].NodeID = p.nodeA
	input.Hops[1].ListenPort = nil
	tunnel, err = p.service.SaveTunnel(p.ctx, tunnel.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = r.TunnelRule(p.ctx, rule.ID)
	if err != nil || changed.EntryNodeID != p.nodeC || changed.Target.Host != "203.0.113.20" || changed.Status != "modified" || changed.UpdatedAt == synchronizedStamp {
		t.Fatal(changed, err)
	}
	if check, e := r.CheckPort(p.ctx, p.nodeA, 30000, nil); e != nil || check.Free {
		t.Fatal(check, e)
	}
	filtered, err := r.TunnelRules(p.ctx, tunnel.ID, p.nodeC)
	if err != nil || len(filtered) != 1 {
		t.Fatal(filtered, err)
	}
	if err = p.service.DeleteTunnelRule(p.ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	changed, err = r.TunnelRule(p.ctx, rule.ID)
	if err != nil || changed.Status != "pending_delete" {
		t.Fatal(changed, err)
	}
	_, err = p.service.SaveTunnelRule(p.ctx, rule.ID, changed.TunnelRuleInput)
	requireTunnelError(t, err, "pending_delete")
	if err = p.service.DeleteTunnel(p.ctx, tunnel.ID); err == nil {
		t.Fatal("never-published tunnel removed despite pending rule FK")
	}
}

func TestTunnelValidation(t *testing.T) {
	p := newPublishPanel(t)
	tunnelTestRange(t, p.canvasPanel, p.nodeA, p.nodeB)
	for _, change := range []func(*TunnelInput){
		func(i *TunnelInput) { i.Hops[1].NodeID = i.Hops[0].NodeID },
		func(i *TunnelInput) { i.Hops[1].Role = "mid" },
		func(i *TunnelInput) { i.Transport = "udp" },
		func(i *TunnelInput) { i.Hops[1].ConnectHost = "bad host" },
	} {
		input := tunnelTestInput(p.nodeA, p.nodeB)
		change(&input)
		_, err := p.service.SaveTunnel(p.ctx, "", input)
		requireTunnelError(t, err, "invalid")
	}
	input := tunnelTestInput(p.nodeA, p.nodeC)
	_, err := p.service.SaveTunnel(p.ctx, "", input)
	requireTunnelError(t, err, "no_free_port")
	input.Hops[1].PortMode = "manual"
	_, err = p.service.SaveTunnel(p.ctx, "", input)
	requireTunnelError(t, err, "port_out_of_range")
	input.Hops[1].ListenPort = canvasTestPort(65536)
	_, err = p.service.SaveTunnel(p.ctx, "", input)
	requireTunnelError(t, err, "port_out_of_range")
	input = tunnelTestInput(p.nodeA, p.nodeB)
	tunnel, err := p.service.SaveTunnel(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	input.Name = "three nodes"
	input.Hops = []TunnelHop{{Role: "entry", NodeID: p.nodeA, PortMode: "auto"}, {Role: "mid", NodeID: p.nodeB, PortMode: "manual", ListenPort: canvasTestPort(30001)}, {Role: "exit", NodeID: p.nodeC, PortMode: "manual", ListenPort: canvasTestPort(12345)}}
	if _, err = p.service.SaveTunnel(p.ctx, "", input); err != nil {
		t.Fatal(err)
	}
	ruleInput := TunnelRuleInput{Name: "rule", EntryNodeID: p.nodeB, PortMode: "manual", ListenPort: canvasTestPort(12346), Network: "tcp", TunnelID: &tunnel.ID, Target: TunnelTarget{Mode: "linked", InboundID: p.inbound.ID}}
	_, err = p.service.SaveTunnelRule(p.ctx, "", ruleInput)
	requireTunnelError(t, err, "invalid")
	ruleInput.EntryNodeID = p.nodeA
	ruleInput.Target.InboundID = "missing"
	_, err = p.service.SaveTunnelRule(p.ctx, "", ruleInput)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	ruleInput.Target = TunnelTarget{Mode: "manual", Host: "bad host", Port: 443}
	_, err = p.service.SaveTunnelRule(p.ctx, "", ruleInput)
	requireTunnelError(t, err, "invalid")
	ruleInput.Target = TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}
	ruleInput.SubscriptionVisible = true
	rule, err := p.service.SaveTunnelRule(p.ctx, "", ruleInput)
	if err != nil || rule.SubscriptionVisible {
		t.Fatal(rule, err)
	}
	ruleInput = rule.TunnelRuleInput
	ruleInput.UpdatedAt = "stale"
	_, err = p.service.SaveTunnelRule(p.ctx, rule.ID, ruleInput)
	requireTunnelError(t, err, "stale")
	if err = p.service.DeleteTunnelRule(p.ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	check, err := p.service.Repository.CheckPort(p.ctx, p.nodeA, 12346, nil)
	if err != nil || !check.Free {
		t.Fatal(check, err)
	}
}

func TestLinkedRuleRejectsInternalForwardAndMissingAddress(t *testing.T) {
	p := newCanvasPanel(t)
	r := p.service.Repository
	input := TunnelRuleInput{Name: "linked", EntryNodeID: p.nodeA, PortMode: "manual", ListenPort: canvasTestPort(31000), Network: "tcp", Target: TunnelTarget{Mode: "linked", InboundID: p.inbound.ID}, Enabled: true}
	_, err := p.service.SaveTunnelRule(p.ctx, "", input)
	requireTunnelError(t, err, "invalid")
	forward, err := r.SaveInbound(p.ctx, "", InboundInput{Name: "forward", NodeID: p.nodeB, Protocol: "forward", Listen: "0.0.0.0", Port: 31001, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp"}}})
	if err != nil {
		t.Fatal(err)
	}
	input.Target.InboundID = forward.ID
	_, err = p.service.SaveTunnelRule(p.ctx, "", input)
	requireTunnelError(t, err, "invalid")
	destination, err := r.SaveInbound(p.ctx, "", InboundInput{Name: "user", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 31002, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	input.Target.InboundID = destination.ID
	if err = (&node.Repository{Database: p.database}).SetPublicIP(p.ctx, p.nodeB, ""); err != nil {
		t.Fatal(err)
	}
	_, err = p.service.SaveTunnelRule(p.ctx, "", input)
	requireTunnelError(t, err, "invalid")
	tunnelInput := tunnelTestInput(p.nodeA, p.nodeB)
	tunnelInput.Hops[1].ListenPort = canvasTestPort(31003)
	tunnel, err := p.service.SaveTunnel(p.ctx, "", tunnelInput)
	if err != nil {
		t.Fatal(err)
	}
	input.TunnelID = &tunnel.ID
	rule, err := p.service.SaveTunnelRule(p.ctx, "", input)
	if err != nil || rule.Target.Host != "127.0.0.1" {
		t.Fatal(rule, err)
	}
	if _, err = p.database.Exec("UPDATE nodes SET deleted_at=? WHERE id=?", timestamp(), p.nodeB); err != nil {
		t.Fatal(err)
	}
	tunnelInput.Name = "deleted destination"
	_, err = p.service.SaveTunnel(p.ctx, "", tunnelInput)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPortServiceReportsCanvasAndRuleOwners(t *testing.T) {
	p := newPublishPanel(t)
	r := p.service.Repository
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	if _, _, err := p.publishScheme(t, "port owner", layout); err != nil {
		t.Fatal(err)
	}
	check, err := r.CheckPort(p.ctx, p.nodeB, 8443, nil)
	if err != nil || check.Free || check.UsedBy.Kind != "canvas" {
		t.Fatal(check, err)
	}
	owner := *check.UsedBy
	check, err = r.CheckPort(p.ctx, p.nodeB, 8443, &owner)
	if err != nil || !check.Free {
		t.Fatal(check, err)
	}
	input := TunnelRuleInput{Name: "rule owner", EntryNodeID: p.nodeA, PortMode: "manual", ListenPort: canvasTestPort(31000), Network: "both", Target: TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}, Enabled: true}
	rule, err := p.service.SaveTunnelRule(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	check, err = r.CheckPort(p.ctx, p.nodeA, 31000, nil)
	if err != nil || check.Free || check.UsedBy.Kind != "rule" || check.UsedBy.ID != rule.ID || check.UsedBy.Name != rule.Name {
		t.Fatal(check, err)
	}
	_, err = r.SaveInbound(p.ctx, "", InboundInput{Name: "claimed", NodeID: p.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 31000, Enabled: true, DefaultOutbound: "direct"})
	if err == nil {
		t.Fatal("inbound took rule's port")
	}
	input.Name = "competing rule"
	_, err = p.service.SaveTunnelRule(p.ctx, "", input)
	requireTunnelError(t, err, "port_in_use")
	if err = ValidateTunnels(p.ctx, p.database, r.Vault); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasAutoPortKeepsLegacyDefault(t *testing.T) {
	p := newPublishPanel(t)
	hop := canvasTestHop("i-a", "i-b", 0)
	hop.TunnelPort = nil
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{hop})
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "no fallback", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !plan.OK || plan.Servers[0].Creates[0].Port != 8443 {
		t.Fatal(plan, err)
	}
}

func TestSharedPortsAcrossInboundsCanvasAndDrafts(t *testing.T) {
	p := newPublishPanel(t)
	r := p.service.Repository
	tunnelTestRange(t, p.canvasPanel, p.nodeA, p.nodeB)
	end := 30002
	encoded, err := r.encode(desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp", PortEnd: &end}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec(`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('range','disabled range',?,'forward','0.0.0.0',30000,0,0,0,'direct','[]',?,'now')`, p.nodeB, encoded); err != nil {
		t.Fatal(err)
	}
	ports, err := r.AllocatePorts(p.ctx, p.nodeB, 2, map[int]bool{30004: true})
	if err != nil || len(ports) != 2 || ports[0] != 30003 || ports[1] != 30005 {
		t.Fatal(ports, err)
	}
	input := tunnelTestInput(p.nodeA, p.nodeB)
	input.Hops[1].ListenPort = canvasTestPort(30001)
	_, err = p.service.SaveTunnel(p.ctx, "", input)
	requireTunnelError(t, err, "port_in_use")
	input.Hops[1].PortMode = "manual"
	manual, err := p.service.SaveTunnel(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.service.DeleteTunnel(p.ctx, manual.ID); err != nil {
		t.Fatal(err)
	}
	input.Hops[1].ListenPort = nil
	input.Hops[1].PortMode = "auto"
	tunnel, err := p.service.SaveTunnel(p.ctx, "", input)
	if err != nil || *tunnel.Hops[1].ListenPort != 30003 {
		t.Fatal(tunnel, err)
	}
	_, err = r.SaveInbound(p.ctx, "", InboundInput{Name: "conflict", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 30003, DefaultOutbound: "direct"})
	if err == nil {
		t.Fatal("inbound reused a draft tunnel claim")
	}
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 30003)})
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "claim conflict", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || plan.OK || len(plan.Conflicts) != 1 {
		t.Fatal(plan, err)
	}
	if err = ValidateTunnels(p.ctx, p.database, r.Vault); err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("DELETE FROM port_claims WHERE owner_id=?", tunnel.ID); err != nil {
		t.Fatal(err)
	}
	if err = ValidateTunnels(p.ctx, p.database, r.Vault); err == nil {
		t.Fatal("missing claim accepted")
	}
}

func assertTunnelHTTP(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || (code != "" && !strings.Contains(response.Body.String(), `"code":"`+code+`"`)) {
		t.Fatalf("want %d %s; got %d %s", status, code, response.Code, response.Body.String())
	}
}
func TestTunnelHandlersPermissionsAndLifecycle(t *testing.T) {
	p := newCanvasPanel(t)
	tunnelTestRange(t, p, p.nodeA, p.nodeB)
	owner, csrf := p.session("Owner")
	viewer, viewerCSRF := p.session("Viewer")
	input := tunnelTestInput(p.nodeA, p.nodeB)
	for _, path := range []string{"/api/v1/tunnels", "/api/v1/tunnel-rules", "/api/v1/nodes/" + p.nodeA + "/ports/check?port=30000"} {
		assertTunnelHTTP(t, p.call("GET", path, nil, viewer, ""), 200, "")
		assertTunnelHTTP(t, p.call("GET", path, nil, nil, ""), 401, "unauthorized")
	}
	assertTunnelHTTP(t, p.call("POST", "/api/v1/tunnels", input, viewer, viewerCSRF), 403, "forbidden")
	assertTunnelHTTP(t, p.call("POST", "/api/v1/tunnels", input, owner, ""), 403, "csrf_failed")
	response := p.call("POST", "/api/v1/tunnels", input, owner, csrf)
	assertTunnelHTTP(t, response, 201, "")
	var tunnel Tunnel
	if err := json.Unmarshal(response.Body.Bytes(), &tunnel); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/tunnels/" + tunnel.ID
	assertTunnelHTTP(t, p.call("PUT", path, input, owner, csrf), 409, "stale")
	input = tunnel.TunnelInput
	response = p.call("PUT", path, input, owner, csrf)
	assertTunnelHTTP(t, response, 200, "")
	if err := json.Unmarshal(response.Body.Bytes(), &tunnel); err != nil {
		t.Fatal(err)
	}
	assertTunnelHTTP(t, p.call("POST", path+"/rotate-credentials", map[string]string{"updatedAt": "old"}, owner, csrf), 409, "stale")
	assertTunnelHTTP(t, p.call("POST", path+"/rotate-credentials", map[string]string{"updatedAt": tunnel.UpdatedAt}, owner, csrf), 200, "")
	assertTunnelHTTP(t, p.call("GET", "/api/v1/nodes/"+p.nodeA+"/ports/check?port=0", nil, viewer, ""), 400, "port_out_of_range")
	assertTunnelHTTP(t, p.call("POST", "/api/v1/nodes/"+p.nodeA+"/ports/allocate", map[string]int{"count": 2}, owner, csrf), 200, "")
	assertTunnelHTTP(t, p.call("POST", "/api/v1/nodes/"+p.nodeA+"/ports/allocate", map[string]int{"count": 7}, owner, csrf), 409, "no_free_port")
	conflicting := TunnelRuleInput{Name: "occupied inbound", EntryNodeID: p.nodeB, ListenPort: canvasTestPort(1080), PortMode: "manual", Network: "tcp", Target: TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}}
	response = p.call("POST", "/api/v1/tunnel-rules", conflicting, owner, csrf)
	assertTunnelHTTP(t, response, 409, "port_in_use")
	if !strings.Contains(response.Body.String(), `"usedBy"`) || !strings.Contains(response.Body.String(), p.inbound.ID) {
		t.Fatal(response.Body.String())
	}
	ruleInput := TunnelRuleInput{Name: "rule", EntryNodeID: p.nodeA, PortMode: "auto", Network: "both", TunnelID: &tunnel.ID, Target: TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}, Enabled: true}
	response = p.call("POST", "/api/v1/tunnel-rules", ruleInput, owner, csrf)
	assertTunnelHTTP(t, response, 201, "")
	var rule TunnelRule
	if err := json.Unmarshal(response.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}
	assertTunnelHTTP(t, p.call("PUT", "/api/v1/tunnel-rules/"+rule.ID, ruleInput, owner, csrf), 409, "stale")
	assertTunnelHTTP(t, p.call("DELETE", path, nil, owner, csrf), 409, "tunnel_in_use")
	response = p.call("GET", "/api/v1/tunnel-rules?tunnelId="+tunnel.ID+"&nodeId="+p.nodeA, nil, viewer, "")
	assertTunnelHTTP(t, response, 200, "")
	if !strings.Contains(response.Body.String(), rule.ID) {
		t.Fatal(response.Body.String())
	}
	markManagedPublished(t, p, "tunnel_rules", rule.ID)
	assertTunnelHTTP(t, p.call("DELETE", "/api/v1/tunnel-rules/"+rule.ID, nil, owner, csrf), 204, "")
	assertTunnelHTTP(t, p.call("PUT", "/api/v1/tunnel-rules/"+rule.ID, rule.TunnelRuleInput, owner, csrf), 409, "pending_delete")
	markManagedPublished(t, p, "tunnels", tunnel.ID)
	assertTunnelHTTP(t, p.call("DELETE", path, nil, owner, csrf), 409, "tunnel_in_use")
	// Simulate X1b completing the rule's removal before deleting its tunnel.
	if _, err := p.database.Exec("DELETE FROM port_claims WHERE owner_kind='rule' AND owner_id=?", rule.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("DELETE FROM tunnel_rules WHERE id=?", rule.ID); err != nil {
		t.Fatal(err)
	}
	assertTunnelHTTP(t, p.call("DELETE", path, nil, owner, csrf), 204, "")
	assertTunnelHTTP(t, p.call("PUT", path, input, owner, csrf), 409, "pending_delete")
	assertTunnelHTTP(t, p.call("POST", path+"/rotate-credentials", map[string]string{"updatedAt": tunnel.UpdatedAt}, owner, csrf), 409, "pending_delete")
	if len(p.audit.actions) < 7 {
		t.Fatal(p.audit.actions)
	}
}

package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	subproxy "proxycontrol/internal/panel/subscription/proxy"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func tunnelTestImport(t *testing.T, p *publishPanel, key, host string, port int) {
	t.Helper()
	raw, err := json.Marshal(subproxy.Proxy{Name: key, Protocol: "hysteria2", Server: host, Port: port, Credential: subproxy.Credential{Password: "import-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := p.service.Repository.Vault.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec(`INSERT OR IGNORE INTO subscription_sources(id,name,url_encrypted,interval_seconds,created_at,updated_at) VALUES('import-source','imports',?,3600,'2026-11-16T00:00:00Z','2026-11-16T00:00:00Z')`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec(`INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at) VALUES(?,'import-source',?,'hysteria2',?,'2026-11-16T00:00:00Z')`, key, key, sealed); err != nil {
		t.Fatal(err)
	}
}

func tunnelTestImportTarget(t *testing.T, key string) TunnelTarget {
	t.Helper()
	var target TunnelTarget
	data, _ := json.Marshal(map[string]string{"mode": "import", "importId": key})
	if err := json.Unmarshal(data, &target); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestImportTunnelRuleLifecycle(t *testing.T) {
	p := newPublishPanel(t)
	tunnelTestImport(t, p, "import-one", "origin.example", 443)
	input := TunnelRuleInput{Name: "import relay", EntryNodeID: p.nodeB, ListenPort: canvasTestPort(31000), PortMode: "manual", Target: tunnelTestImportTarget(t, "import-one"), Enabled: true, SubscriptionVisible: true}
	rule, err := p.service.SaveTunnelRule(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if rule.Target.Host != "origin.example" || rule.Target.Port != 443 || rule.Target.Protocol != "hysteria2" || rule.Target.NodeName != "import-one" || rule.Network != "both" || !rule.SubscriptionVisible {
		t.Fatal(rule)
	}
	var storedHost, storedPort any
	if err = p.database.QueryRow(`SELECT target_host,target_port FROM tunnel_rules WHERE id=?`, rule.ID).Scan(&storedHost, &storedPort); err != nil || storedHost != nil || storedPort != nil {
		t.Fatal(storedHost, storedPort, err)
	}
	input = rule.TunnelRuleInput
	input.Name = "renamed rule"
	rule, err = p.service.SaveTunnelRule(p.ctx, rule.ID, input)
	if err != nil || rule.Name != "renamed rule" {
		t.Fatal(rule, err)
	}
	caps, _ := json.Marshal(&pb.Capabilities{CoreName: "xray", ForwardInboundSupported: true})
	if _, err = p.database.Exec(`UPDATE nodes SET capabilities_json=? WHERE id=?`, string(caps), p.nodeB); err != nil {
		t.Fatal(err)
	}
	publication, err := p.service.buildTunnelPublication(p.ctx, PublishSelection{RuleIDs: []string{rule.ID}})
	if err != nil {
		t.Fatal(err)
	}
	f := publication.desired[0].input.Settings.Forward
	if f.Host != "origin.example" || f.Port != 443 || f.Network != "tcp,udp" {
		t.Fatal(f)
	}
	p.service.Deploy = &relayReceipt{database: p.database, apply: func() error { return nil }}
	result, err := p.service.PublishTunnels(p.ctx, PublishSelection{RuleIDs: []string{rule.ID}}, "owner", p.users)
	if err != nil || len(result.Servers) != 1 || result.Servers[0].Status != "applied" {
		t.Fatal(result, err)
	}
	var snapshot string
	if err = p.database.QueryRow(`SELECT published_snapshot FROM tunnel_rules WHERE id=?`, rule.ID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot, `"importId":"import-one"`) || strings.Contains(snapshot, "import-secret") {
		t.Fatal(snapshot)
	}
	items, err := p.service.Repository.NodeRelayRules(p.ctx, p.nodeB)
	if err != nil || len(items) != 1 || items[0].ID != rule.ID {
		t.Fatal(items, err)
	}
	if _, err = p.database.Exec(`DELETE FROM imported_proxies WHERE id='import-one'`); err != nil {
		t.Fatal(err)
	}
	rule, err = p.service.Repository.TunnelRule(p.ctx, rule.ID)
	if err != nil || rule.Status != "needs_publish" || rule.Target.NodeName != "目标已删除" {
		t.Fatal(rule, err)
	}
	_, err = p.service.PlanTunnels(p.ctx, PublishSelection{RuleIDs: []string{rule.ID}})
	requireTunnelError(t, err, "import_target_deleted")
}

func TestImportTunnelRuleRejectsMissingTarget(t *testing.T) {
	p := newPublishPanel(t)
	_, err := p.service.SaveTunnelRule(p.ctx, "", TunnelRuleInput{Name: "missing", EntryNodeID: p.nodeA, ListenPort: canvasTestPort(31000), PortMode: "manual", Network: "both", Target: tunnelTestImportTarget(t, "missing")})
	requireTunnelError(t, err, "import_target_deleted")
}

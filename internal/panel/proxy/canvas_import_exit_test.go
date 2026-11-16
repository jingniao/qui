package proxy

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	subproxy "proxycontrol/internal/panel/subscription/proxy"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// canvasTestImport stores one imported endpoint in the panel cache.
func canvasTestImport(t *testing.T, p *publishPanel, key string, endpoint subproxy.Proxy) {
	t.Helper()
	endpoint.Name = key
	raw, err := json.Marshal(endpoint)
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
	if _, err = p.database.Exec(`INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at) VALUES(?,'import-source',?,?,?,'2026-11-16T00:00:00Z')`, key, key, endpoint.Protocol, sealed); err != nil {
		t.Fatal(err)
	}
}

// canvasTestNodeCapabilities edits the entry node's capability report.
func canvasTestNodeCapabilities(t *testing.T, p *publishPanel, edit func(*pb.Capabilities)) {
	t.Helper()
	n, err := p.service.Nodes.Get(p.ctx, p.nodeA)
	if err != nil {
		t.Fatal(err)
	}
	edit(n.Capabilities)
	raw, err := json.Marshal(n.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), p.nodeA); err != nil {
		t.Fatal(err)
	}
}

func canvasImportLayout(p *publishPanel, importID string) CanvasLayout {
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "inbound:" + p.inbound.ID, Role: "entry"}, {InstanceID: "exit", Ref: "import:" + importID, Role: "exit"}}, Connections: []CanvasConnection{canvasTestHop("entry", "exit", 8443)}}
	layout.Connections[0].SupportUDP = false
	return layout
}

func TestCanvasImportExitGeneratesOwnedOutbound(t *testing.T) {
	p := newPublishPanel(t)
	canvasTestNodeCapabilities(t, p, func(c *pb.Capabilities) {
		c.Protocols = append(c.Protocols, &pb.ProtocolCapability{Protocol: "trojan", Inbound: true, Outbound: true, Tcp: true})
	})
	canvasTestImport(t, p, "home-trojan", subproxy.Proxy{Protocol: "trojan", Server: "198.51.100.9", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "import-secret"}, TLS: &subproxy.TLS{ServerName: "home.example.com"}})
	scheme := savePlanScheme(t, p, canvasImportLayout(p, "home-trojan"))
	preview, err := p.service.PlanCanvas(p.ctx, scheme)
	if err != nil || !preview.OK {
		t.Fatalf("plan %+v %v", preview, err)
	}
	listed := false
	for _, server := range preview.Servers {
		for _, object := range server.Creates {
			if object.Kind == "outbound" && strings.Contains(object.Name, "导入出口 home-trojan") && object.Target == "导入节点 home-trojan" {
				listed = true
			}
		}
	}
	if !listed {
		t.Fatalf("preview does not list the generated outbound: %+v", preview.Servers)
	}
	if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	scheme, err = p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	tunnelID := scheme.Layout.Connections[0].TunnelID
	out, err := p.service.Repository.canvasImportOutbound(p.ctx, tunnelID, p.nodeA)
	if err != nil {
		t.Fatal("generated outbound missing", err)
	}
	if out.Protocol != "trojan" || out.Address != "198.51.100.9" || out.Port != 443 || out.Settings.ServerName != "home.example.com" || !out.SupportTCP || out.SupportUDP {
		t.Fatalf("generated outbound %+v", out)
	}
	entry, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, true)
	if err != nil || entry.DefaultOutbound != out.ID {
		t.Fatalf("entry routes to %q, want %q (%v)", entry.DefaultOutbound, out.ID, err)
	}
	// A republication reuses the same object.
	scheme, err = p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	again, err := p.service.Repository.canvasImportOutbound(p.ctx, tunnelID, p.nodeA)
	if err != nil || again.ID != out.ID {
		t.Fatalf("republication replaced the outbound: %v", err)
	}
	if err := p.service.Repository.refuseManagedImportOutbound(p.ctx, out.ID); err == nil || !strings.Contains(err.Error(), "中转方案") {
		t.Fatalf("owned outbound editable: %v", err)
	}
	if _, err = p.database.Exec("UPDATE outbounds SET name='renamed owned outbound' WHERE id=?", out.ID); err != nil {
		t.Fatal(err)
	}
	// A source refresh changes the plan without touching the active outbound.
	updatedEndpoint := subproxy.Proxy{Protocol: "trojan", Server: "198.51.100.10", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "rotated-secret"}, TLS: &subproxy.TLS{ServerName: "home.example.com"}}
	raw, err := json.Marshal(updatedEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := p.service.Repository.Vault.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("UPDATE imported_proxies SET payload_encrypted=? WHERE id='home-trojan'", sealed); err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged, err := p.service.canvasPublicationUnchanged(p.ctx, plan); err != nil || unchanged {
		t.Fatalf("refreshed import was treated as unchanged: %v", err)
	}
	if reasons, err := p.service.canvasImportDraftReasons(p.ctx, scheme); err != nil || len(reasons) != 1 || reasons[0] != "导入节点已更新，重新发布后生效" {
		t.Fatalf("refresh not shown in draft: %v %v", reasons, err)
	}
	before, err := p.service.Repository.Outbound(p.ctx, out.ID, true)
	if err != nil || before.Address != out.Address {
		t.Fatalf("refresh changed active outbound: %v", err)
	}
	// A later materialization failure must restore the old secret, not retain
	// the newly written one. Exercise the existing compensator directly.
	undo := &canvasUndo{}
	if err := p.service.canvasEnsureImportExits(p.ctx, plan, undo); err != nil {
		t.Fatal(err)
	}
	if err := undo.run(p.ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := p.service.Repository.Outbound(p.ctx, out.ID, true)
	if err != nil || restored.CredentialID != before.CredentialID || restored.Address != before.Address {
		t.Fatalf("import compensation did not restore old outbound: %v", err)
	}
	if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	after, err := p.service.Repository.Outbound(p.ctx, out.ID, true)
	if err != nil || after.Address != updatedEndpoint.Server {
		t.Fatalf("refreshed address not applied: %v", err)
	}
	credential, err := p.service.Repository.Credential(p.ctx, after.CredentialID)
	if err != nil || credential.Secret != "rotated-secret" {
		t.Fatalf("refreshed credential not applied: %v", err)
	}
	scheme, err = p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reasons, err := p.service.canvasImportDraftReasons(p.ctx, scheme); err != nil || len(reasons) != 0 {
		t.Fatalf("published import still dirty: %v %v", reasons, err)
	}
	if _, err := p.database.Exec("DELETE FROM imported_proxies WHERE id='home-trojan'"); err != nil {
		t.Fatal(err)
	}
	if reasons, err := p.service.canvasImportDraftReasons(p.ctx, scheme); err != nil || len(reasons) != 1 || !strings.Contains(reasons[0], "已不存在") {
		t.Fatalf("missing import not shown: %v %v", reasons, err)
	}
	if _, err := p.service.canvasBuildPlan(p.ctx, scheme); err == nil || !strings.Contains(err.Error(), "已不存在") {
		t.Fatalf("missing import can publish: %v", err)
	}
	// Dropping the import from the drawing prunes its outbound.
	layout := scheme.Layout
	layout.Instances = layout.Instances[:1]
	layout.Connections = nil
	if _, err = p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: scheme.Name, Execution: scheme.Execution, Layout: layout, UpdatedAt: scheme.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.Repository.canvasImportOutbound(p.ctx, tunnelID, p.nodeA); err == nil {
		t.Fatal("stale import outbound was not removed")
	}
}

func TestCanvasImportExitRealityNeedsOutboundReport(t *testing.T) {
	p := newPublishPanel(t)
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	shortID := "0123abcd"
	canvasTestImport(t, p, "home-reality", subproxy.Proxy{Protocol: "vless", Server: "23.246.167.253", Port: 36514, Security: "reality", Flow: "xtls-rprx-vision",
		Credential: subproxy.Credential{Password: "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"}, TLS: &subproxy.TLS{ServerName: "www.example.com"},
		Reality: &subproxy.RealityOptions{PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: &shortID}})
	// Real Agents report the core as "xray"; the fixture's banner name is not one.
	canvasTestNodeCapabilities(t, p, func(c *pb.Capabilities) { c.CoreName = desiredstate.CoreXray })
	scheme := savePlanScheme(t, p, canvasImportLayout(p, "home-reality"))
	if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil || !strings.Contains(err.Error(), "请先升级该服务器的 Agent") {
		t.Fatalf("an Agent without the outbound report accepted Reality client settings: %v", err)
	}
	canvasTestNodeCapabilities(t, p, func(c *pb.Capabilities) {
		c.ServerSettingsContractVersion = desiredstate.SettingsContractVersion
		for _, combination := range desiredstate.OutboundClientCombinations() {
			c.ServerSettingsCombinations = append(c.ServerSettingsCombinations, &pb.SettingsCombination{Protocol: combination.Protocol, Direction: combination.Direction, Security: combination.Security, Transport: combination.Transport, Flow: combination.Flow})
		}
		for _, core := range c.Cores {
			core.ServerSettingsContractVersion, core.ServerSettingsCombinations = c.ServerSettingsContractVersion, c.ServerSettingsCombinations
		}
	})
	if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	scheme, err = p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.service.Repository.canvasImportOutbound(p.ctx, scheme.Layout.Connections[0].TunnelID, p.nodeA)
	if err != nil {
		t.Fatal(err)
	}
	if r := out.Settings.RealityClient; r == nil || r.ShortID != shortID || out.Settings.ClientFingerprint != "chrome" || out.Settings.Flow != "xtls-rprx-vision" {
		t.Fatalf("Reality client settings %+v", out.Settings)
	}
}

func TestCanvasImportExitRefusesLossyEndpoints(t *testing.T) {
	p := newPublishPanel(t)
	canvasTestImport(t, p, "insecure", subproxy.Proxy{Protocol: "trojan", Server: "198.51.100.9", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "p"}, TLS: &subproxy.TLS{Insecure: true}})
	scheme := savePlanScheme(t, p, canvasImportLayout(p, "insecure"))
	if _, err := p.service.canvasBuildPlan(p.ctx, scheme); err == nil || !strings.Contains(err.Error(), "跳过了证书校验") {
		t.Fatalf("insecure import planned: %v", err)
	}
	layout := canvasImportLayout(p, "insecure")
	layout.Connections[0].Source, layout.Connections[0].Target = "exit", "entry"
	if err := p.service.validateCanvasExits(p.ctx, layout); err == nil {
		t.Fatal("an import started a connection")
	}
}

func TestCanvasImportExitsReportConvertibility(t *testing.T) {
	p := newPublishPanel(t)
	canvasTestImport(t, p, "a-good", subproxy.Proxy{Protocol: "socks5", Server: "198.51.100.1", Port: 1080})
	canvasTestImport(t, p, "b-insecure", subproxy.Proxy{Protocol: "trojan", Server: "198.51.100.9", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "p"}, TLS: &subproxy.TLS{Insecure: true}})
	items, err := p.service.CanvasImportExits(p.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "a-good" || items[0].Reason != "" || items[1].ID != "b-insecure" || !strings.Contains(items[1].Reason, "证书校验") {
		t.Fatalf("%+v", items)
	}
}

func TestQuickCanvasImportExitCreatesEntryAndOutbound(t *testing.T) {
	p, input := quickCanvasFixture(t)
	// Saving a group that lists an import opens its source; production wires the vault.
	p.service.Nodes.(*node.Repository).Vault = p.service.Repository.Vault
	canvasTestImport(t, p, "home-socks", subproxy.Proxy{Protocol: "socks5", Server: "198.51.100.1", Port: 1080, Credential: subproxy.Credential{Username: "u", Password: "p"}})
	if _, err := p.database.Exec(`INSERT INTO node_group_imports(group_id,source_id,proxy_key) VALUES('exit-group','import-source','home-socks|socks5')`); err != nil {
		t.Fatal(err)
	}
	input.ExitInboundID, input.ExitImportID = "", "home-socks"
	preview, err := p.service.PlanQuickCanvas(p.ctx, input)
	if err != nil || !preview.OK {
		t.Fatalf("plan %+v %v", preview, err)
	}
	if len(preview.Entries) != 1 || len(preview.Entries[0].Groups) != 1 || preview.Entries[0].Groups[0].ID != "exit-group" {
		t.Fatalf("an entry dialling an import must join the groups containing it: %+v", preview.Entries)
	}
	viaMiddle := input
	viaMiddle.RequestID, viaMiddle.MiddleNodeIDs = "quick-middle", []string{p.nodeC}
	if middle, err := p.service.PlanQuickCanvas(p.ctx, viaMiddle); err != nil || !middle.OK || len(middle.Entries) != 1 || len(middle.Entries[0].Groups) != 1 {
		t.Fatalf("a relayed import exit must lend its groups too: %+v %v", middle, err)
	}
	result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	scheme, err := p.service.Repository.CanvasScheme(p.ctx, result.SchemeID)
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.service.Repository.canvasImportOutbound(p.ctx, scheme.Layout.Connections[len(scheme.Layout.Connections)-1].TunnelID, p.nodeA)
	if err != nil {
		t.Fatal("generated outbound missing", err)
	}
	entry, err := p.service.Repository.Inbound(p.ctx, result.Publication.Entries[0].InboundID, true)
	if err != nil || entry.DefaultOutbound != out.ID {
		t.Fatalf("entry routes to %q, want %q (%v)", entry.DefaultOutbound, out.ID, err)
	}
	both := input
	both.ExitInboundID = "x"
	if _, err := p.service.PlanQuickCanvas(p.ctx, both); err == nil {
		t.Fatal("two exits were accepted")
	}
	removed, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt}, "Owner", p.users)
	if err != nil || !removed.Removed {
		t.Fatalf("import scheme removal failed: %+v %v", removed, err)
	}
	if _, err := p.service.Repository.Outbound(p.ctx, out.ID, true); err == nil {
		t.Fatal("scheme removal left imported outbound")
	}
}

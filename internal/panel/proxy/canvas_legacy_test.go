package proxy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCanvasLegacyEmptyTunnelsDoNotPolluteDetachedResources(t *testing.T) {
	p, scheme, _, _ := removeFixture(t, "create")
	tunnelID := scheme.Layout.Connections[0].TunnelID
	// Historical S1 residue has S2's endpoints but no owned objects.
	for _, emptyID := range []string{"empty-s1", "empty-s0"} {
		if _, err := p.database.Exec(`INSERT INTO canvas_tunnels(id,name,connection_json,source_ref,target_ref,object_prefix,created_at,updated_at,published_version)
 SELECT ?,?,connection_json,source_ref,target_ref,?,created_at,updated_at,updated_at FROM canvas_tunnels WHERE id=?`, emptyID, emptyID, "["+emptyID+"]", tunnelID); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.service.Repository.DeleteCanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
	resources, err := p.service.LegacyCanvasResources(p.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Detached) != 1 {
		t.Fatalf("detached: %+v", resources.Detached)
	}
	g := resources.Detached[0]
	if len(g.TunnelIDs) != 1 || g.TunnelIDs[0] != tunnelID || !g.CanReattach || !g.CanRemove {
		t.Fatalf("polluted group: %+v", g)
	}
	if len(resources.OrphanTunnels) != 2 {
		t.Fatalf("empty tunnels not separated: %+v", resources.OrphanTunnels)
	}
	for _, empty := range resources.OrphanTunnels {
		if len(empty.TunnelIDs) != 1 || !empty.CanRemove || empty.CanReattach {
			t.Fatalf("empty actions: %+v", empty)
		}
		if err := p.service.DeleteCanvasTunnel(p.ctx, empty.TunnelIDs[0]); err != nil {
			t.Fatal(err)
		}
		if _, err := p.service.Repository.canvasTunnel(p.ctx, empty.TunnelIDs[0]); err != ErrNotFound {
			t.Fatalf("empty survived: %v", err)
		}
	}
	restored, err := p.service.ReattachCanvasLegacy(p.ctx, CanvasLegacyReattachInput{Name: "S2 recovered", BindingIDs: g.BindingIDs, TunnelIDs: g.TunnelIDs})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.RemoveCanvasScheme(p.ctx, restored.ID, CanvasRemoveInput{UpdatedAt: restored.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasLegacyReadOnlyAndReattachRemovePlan(t *testing.T) {
	p, _ := quickCanvasFixture(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err = p.service.Repository.markTunnelPublication(p.ctx, scheme.ID, "", plan); err != nil {
		t.Fatal(err)
	}
	if err = p.service.Repository.DeleteCanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
	owner, csrf := p.session("Owner")
	p.database.SetMaxOpenConns(1)
	if _, err = p.database.Exec("UPDATE canvas_tunnel_reconcile SET done=0"); err != nil {
		t.Fatal(err)
	}
	before := quickCanvasChanges(t, p)
	response := p.call("GET", "/api/v1/relay-schemes/legacy-resources", nil, owner, "")
	if response.Code != 200 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	if quickCanvasChanges(t, p) != before {
		t.Fatal("GET wrote database")
	}
	var resources struct {
		Detached []struct {
			BindingIDs  []string `json:"bindingIds"`
			TunnelIDs   []string `json:"tunnelIds"`
			CanReattach bool     `json:"canReattach"`
			CanRemove   bool     `json:"canRemove"`
		} `json:"detached"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &resources); err != nil {
		t.Fatal(err)
	}
	if len(resources.Detached) != 1 || !resources.Detached[0].CanReattach || !resources.Detached[0].CanRemove {
		t.Fatalf("resources %s", response.Body.String())
	}
	g := resources.Detached[0]
	runtimeBefore, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	var revisionsBefore int
	if err = p.database.QueryRow("SELECT count(*) FROM config_revisions").Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}
	callsBefore := p.deployer.calls
	runtimeRows := map[string][][]any{}
	for _, table := range []string{"inbounds", "outbounds", "proxy_chains", "node_group_inbounds", "config_revisions"} {
		rows, e := readCanvasRemovalRows(p.ctx, p.database, "SELECT * FROM "+table+" WHERE ?=''", "")
		if e != nil {
			t.Fatal(e)
		}
		runtimeRows[table] = rows
	}
	response = p.call("POST", "/api/v1/relay-schemes/legacy-resources/reattach", map[string]any{"name": "Recovered", "bindingIds": g.BindingIDs, "tunnelIds": g.TunnelIDs}, owner, csrf)
	if response.Code != 201 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var result struct {
		SchemeID string `json:"schemeId"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	restored, err := p.service.Repository.CanvasScheme(p.ctx, result.SchemeID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.PublishedAt != "" || restored.Execution != "server" || len(restored.Layout.Connections) != 1 || restored.Layout.Connections[0].TunnelID != g.TunnelIDs[0] {
		t.Fatalf("scheme %+v", restored)
	}
	b, err := p.service.Repository.CanvasEntryBinding(p.ctx, result.SchemeID, "i-a")
	if err != nil || b.ID != g.BindingIDs[0] {
		t.Fatalf("binding %+v %v", b, err)
	}
	in, err := p.service.Repository.Inbound(p.ctx, b.InboundID, true)
	if err != nil || in.DefaultOutbound != b.AppliedDefaultOutbound {
		t.Fatalf("runtime %+v %v", in, err)
	}
	if !reflect.DeepEqual(runtimeBefore, in) || p.deployer.calls != callsBefore {
		t.Fatal("reattach changed runtime or published")
	}
	for table, beforeRows := range runtimeRows {
		afterRows, e := readCanvasRemovalRows(p.ctx, p.database, "SELECT * FROM "+table+" WHERE ?=''", "")
		if e != nil || !reflect.DeepEqual(beforeRows, afterRows) {
			t.Fatalf("runtime table changed: %s: %v", table, e)
		}
	}
	var revisionsAfter int
	if err = p.database.QueryRow("SELECT count(*) FROM config_revisions").Scan(&revisionsAfter); err != nil || revisionsBefore != revisionsAfter {
		t.Fatalf("revisions %d -> %d: %v", revisionsBefore, revisionsAfter, err)
	}
	remove, err := p.service.PlanRemoveCanvasScheme(p.ctx, result.SchemeID, CanvasRemoveInput{UpdatedAt: restored.UpdatedAt})
	if err != nil || !remove.OK || len(remove.Entries) != 1 || len(remove.Objects) != 2 {
		t.Fatalf("remove %+v %v", remove, err)
	}
	// Published ownership must survive a later draft edit that drops the edge.
	if _, err = p.database.Exec("UPDATE canvas_schemes SET layout_json=json_set(layout_json,'$.connections',json('[]')) WHERE id=?", result.SchemeID); err != nil {
		t.Fatal(err)
	}
	remove, err = p.service.PlanRemoveCanvasScheme(p.ctx, result.SchemeID, CanvasRemoveInput{UpdatedAt: restored.UpdatedAt})
	if err != nil || len(remove.Objects) != 2 {
		t.Fatalf("published association lost: %+v %v", remove, err)
	}
}

func TestCanvasLegacyThreeCategoriesAndReferencedTunnel(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err = p.service.Repository.DeleteCanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
	// Move the channel away from the detached entry to give an independent orphan component.
	tunnelID := scheme.Layout.Connections[0].TunnelID
	if _, err = p.database.Exec("UPDATE canvas_tunnels SET source_ref=?,target_ref=? WHERE id=?", "server:"+p.nodeB, "server:"+p.nodeC, tunnelID); err != nil {
		t.Fatal(err)
	}
	binding, err := p.service.Repository.CanvasEntryBindingByInbound(p.ctx, p.inbound.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "Other consumer", NodeID: p.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 20001, Enabled: true, DefaultOutbound: binding.AppliedDefaultOutbound})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "[history] object" + canvasInboundSuffix, NodeID: p.nodeC, Protocol: "socks5", Listen: "0.0.0.0", Port: 20002, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.service.LegacyCanvasResources(p.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Detached) != 1 || len(result.OrphanTunnels) != 1 || len(result.Unowned) != 1 || result.Unowned[0].ID != legacy.ID {
		t.Fatalf("categories %+v", result)
	}
	g := result.OrphanTunnels[0]
	if g.CanRemove || !g.CanReattach || len(g.RemoveReasons) == 0 {
		t.Fatalf("referenced %+v", g)
	}
	found := false
	for _, ref := range g.Referrers {
		if ref.ID == other.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing consumer %+v", g.Referrers)
	}
}

func TestCanvasLegacyReattachRejectsDisconnectedAndMissingEndpoints(t *testing.T) {
	p, _ := quickCanvasFixture(t)
	owner, csrf := p.session("Owner")
	one, err := p.service.SaveCanvasTunnel(p.ctx, "", CanvasTunnelInput{Name: "one", Source: "server:" + p.nodeA, Target: "server:" + p.nodeB, Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true})
	if err != nil {
		t.Fatal(err)
	}
	two, err := p.service.SaveCanvasTunnel(p.ctx, "", CanvasTunnelInput{Name: "two", Source: "server:" + p.nodeB, Target: "server:" + p.nodeC, Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("UPDATE canvas_tunnels SET source_ref='server:missing-a',target_ref='server:missing-b' WHERE id=?", two.ID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/relay-schemes/legacy-resources/reattach"
	response := p.call("POST", path, map[string]any{"name": "Disconnected", "tunnelIds": []string{one.ID, two.ID}}, owner, csrf)
	if response.Code != 400 {
		t.Fatalf("disconnected %d %s", response.Code, response.Body.String())
	}
	before := quickCanvasChanges(t, p)
	response = p.call("POST", path, map[string]any{"name": "Missing", "tunnelIds": []string{two.ID}}, owner, csrf)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "missing-a") || !strings.Contains(response.Body.String(), "missing-b") {
		t.Fatalf("missing %d %s", response.Code, response.Body.String())
	}
	if quickCanvasChanges(t, p) != before {
		t.Fatal("invalid reattach wrote database")
	}
}

func TestCanvasLegacyReattachRejectsCycleAndFork(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		t.Run(map[bool]string{false: "fork", true: "cycle"}[cycle], func(t *testing.T) {
			p := newPublishPanel(t)
			a, err := p.service.SaveCanvasTunnel(p.ctx, "", CanvasTunnelInput{Name: "one", Source: "server:" + p.nodeA, Target: "server:" + p.nodeB, Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true})
			if err != nil {
				t.Fatal(err)
			}
			source, target := p.nodeA, p.nodeC
			if cycle {
				source, target = p.nodeB, p.nodeA
			}
			b, err := p.service.SaveCanvasTunnel(p.ctx, "", CanvasTunnelInput{Name: "two", Source: "server:" + source, Target: "server:" + target, Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true})
			if err != nil {
				t.Fatal(err)
			}
			// This tests invalid resource graphs; empty rows are no longer graph edges.
			for index, tunnel := range []CanvasTunnel{a, b} {
				in, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: tunnel.Name, NodeID: p.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 20010 + index, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := p.database.Exec("INSERT INTO canvas_tunnel_objects VALUES(?,'inbound',?)", tunnel.ID, in.ID); err != nil {
					t.Fatal(err)
				}
			}
			_, err = p.service.ReattachCanvasLegacy(p.ctx, CanvasLegacyReattachInput{Name: "Bad layout", TunnelIDs: []string{a.ID, b.ID}})
			if err == nil || (cycle && !strings.Contains(err.Error(), "环路")) || (!cycle && !strings.Contains(err.Error(), "分叉")) {
				t.Fatalf("unexpected %v", err)
			}
		})
	}
}

func TestCanvasLegacyPublishedReferrerProtectsTunnel(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, nil))
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err = p.service.Repository.markTunnelPublication(p.ctx, scheme.ID, "", plan); err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("UPDATE canvas_schemes SET layout_json=json_set(layout_json,'$.connections',json('[]')) WHERE id=?", scheme.ID); err != nil {
		t.Fatal(err)
	}
	result, err := p.service.LegacyCanvasResources(p.ctx)
	if err != nil || len(result.OrphanTunnels) != 1 || result.OrphanTunnels[0].CanRemove {
		t.Fatalf("protected %+v %v", result, err)
	}
	found := false
	for _, ref := range result.OrphanTunnels[0].Referrers {
		if ref.Kind == "scheme" && ref.ID == scheme.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("published referrer missing")
	}
}

func TestCanvasLegacyRoutesOwnerAndCSRF(t *testing.T) {
	p, _ := quickCanvasFixture(t)
	owner, _ := p.session("Owner")
	viewer, csrf := p.session("Viewer")
	for _, method := range []string{"GET", "POST"} {
		path := "/api/v1/relay-schemes/legacy-resources"
		if method == "POST" {
			path += "/reattach"
		}
		response := p.call(method, path, map[string]any{"name": "Recovered"}, viewer, csrf)
		if response.Code != 403 {
			t.Fatalf("%s %d %s", method, response.Code, response.Body.String())
		}
	}
	response := p.call("POST", "/api/v1/relay-schemes/legacy-resources/reattach", map[string]any{"name": "Recovered"}, owner, "")
	if response.Code != 403 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
}

func TestCanvasLegacyReattachThreeHopChainAndCreateBinding(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)})
	layout.Instances[0].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
	scheme := savePlanScheme(t, p, layout)
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err = p.service.Repository.DeleteCanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
	resources, err := p.service.LegacyCanvasResources(p.ctx)
	if err != nil || len(resources.Detached) != 1 {
		t.Fatalf("resources %+v %v", resources, err)
	}
	g := resources.Detached[0]
	restored, err := p.service.ReattachCanvasLegacy(p.ctx, CanvasLegacyReattachInput{Name: "Restored chain", BindingIDs: g.BindingIDs, TunnelIDs: g.TunnelIDs})
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Layout.Instances) != 3 || len(restored.Layout.Connections) != 2 {
		t.Fatalf("layout %+v", restored.Layout)
	}
	remove, err := p.service.PlanRemoveCanvasScheme(p.ctx, restored.ID, CanvasRemoveInput{UpdatedAt: restored.UpdatedAt, AcknowledgeChanges: true})
	if err != nil || !remove.OK || len(remove.Entries) != 1 || remove.Entries[0].Action != "delete" {
		t.Fatalf("remove %+v %v", remove, err)
	}
	chains := 0
	for _, o := range remove.Objects {
		if o.Kind == "chain" {
			chains++
		}
		if o.Keep {
			t.Fatalf("unexpected retained object %+v", o)
		}
	}
	if chains != 1 {
		t.Fatalf("missing chain %+v", remove.Objects)
	}
}

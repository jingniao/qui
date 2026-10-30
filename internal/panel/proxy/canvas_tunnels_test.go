package proxy

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCanvasK4SharedTunnelLifecycle(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	a, _, err := p.publishScheme(t, "shared-a", layout)
	if err != nil {
		t.Fatal(err)
	}
	tunnelID := a.Layout.Connections[0].TunnelID
	if tunnelID == "" {
		t.Fatal(a)
	}
	b, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "shared-b", Execution: "server", Layout: a.Layout})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanCanvasScheme(p.ctx, b.ID, nil)
	if err != nil || !plan.OK || len(plan.Tunnels) != 1 || len(plan.Tunnels[0].AffectedSchemes) != 2 {
		t.Fatal(plan, err)
	}
	calls := p.deployer.calls
	if result, err := p.service.PublishCanvasScheme(p.ctx, b.ID, "Owner", p.users); err != nil || len(result.Nodes) != 0 || p.deployer.calls != calls {
		t.Fatal(result, err, p.deployer.calls)
	}
	if err := p.service.DeleteCanvasTunnel(p.ctx, tunnelID); err == nil {
		t.Fatal("in-use tunnel deleted")
	} else {
		var inUse *TunnelInUseError
		if !errors.As(err, &inUse) || len(inUse.Schemes) != 2 {
			t.Fatal(err)
		}
	}
	tunnel, err := p.service.Repository.canvasTunnel(p.ctx, tunnelID)
	if err != nil {
		t.Fatal(err)
	}
	input := tunnel.CanvasTunnelInput
	input.SupportUDP = false
	edited, err := p.service.SaveCanvasTunnel(p.ctx, tunnelID, input)
	if err != nil || edited.PublishStatus != "modified" {
		t.Fatal(edited, err)
	}
	for _, schemeID := range []string{a.ID, b.ID} {
		scheme, err := p.service.Repository.CanvasScheme(p.ctx, schemeID)
		if err != nil || scheme.Layout.Connections[0].SupportUDP {
			t.Fatal(scheme, err)
		}
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, b.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if err := p.service.Repository.DeleteCanvasScheme(p.ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	objects, err := p.service.Repository.tunnelObjects(p.ctx, tunnelID)
	if err != nil || len(objects) != 2 {
		t.Fatal(objects, err)
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Layout.Connections = nil
	if _, err := p.service.SaveCanvasScheme(p.ctx, b.ID, CanvasSchemeInput{Name: b.Name, Execution: "server", Layout: stored.Layout, UpdatedAt: stored.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, b.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	objects, err = p.service.Repository.tunnelObjects(p.ctx, tunnelID)
	if err != nil || len(objects) != 0 {
		t.Fatal(objects, err)
	}
	if err := p.service.DeleteCanvasTunnel(p.ctx, tunnelID); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasK4EntryRoutingConflict(t *testing.T) {
	p := newPublishPanel(t)
	a, _, err := p.publishScheme(t, "route-a", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}))
	if err != nil {
		t.Fatal(err)
	}
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeC}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 9443)})
	b, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "route-b", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	for _, schemeID := range []string{a.ID, b.ID} {
		plan, err := p.service.PlanCanvasScheme(p.ctx, schemeID, nil)
		if err != nil || plan.OK || !strings.Contains(plan.Message, "entry_routing_conflict") {
			t.Fatal(plan, err)
		}
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, b.ID, "Owner", p.users); err == nil {
		t.Fatal("conflicting entry published")
	}
}

func TestCanvasK4ExplicitAdoptLegacy(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	old, _, err := p.publishScheme(t, "legacy-adopt", layout)
	if err != nil {
		t.Fatal(err)
	}
	canvasK4LegacyDatabase(t, p, old, true)
	replacement, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: old.Name, Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := p.service.Repository.LegacyCanvasObjects(p.ctx)
	if err != nil || len(legacy) != 2 {
		t.Fatal(legacy, err)
	}
	tunnelID := replacement.Layout.Connections[0].TunnelID
	for _, o := range legacy {
		if len(o.CandidateTunnels) != 1 || o.CandidateTunnels[0] != tunnelID {
			t.Fatal(o)
		}
	}
	if err := p.service.Repository.AdoptCanvasObjects(p.ctx, tunnelID, legacy); err != nil {
		t.Fatal(err)
	}
	if err := p.service.Repository.AdoptCanvasObjects(p.ctx, tunnelID, legacy); !errors.Is(err, ErrCanvasObjectOwned) {
		t.Fatal(err)
	}
	plan, err := p.service.PlanCanvasScheme(p.ctx, replacement.ID, nil)
	if err != nil || !plan.OK {
		t.Fatal(plan, err)
	}
	for _, server := range plan.Servers {
		if len(server.Creates)+len(server.Removes) > 0 {
			t.Fatal(server)
		}
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, replacement.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasK4TunnelAPIAndStrictReferences(t *testing.T) {
	p := newCanvasPanel(t)
	owner, csrf := p.session("Owner")
	viewer, vcsrf := p.session("Viewer")
	body := map[string]any{"name": "API tunnel", "source": "server:" + p.nodeA, "target": "server:" + p.nodeB, "type": "tunnel", "tunnelProtocol": "tls", "tunnelPort": 8443, "supportTcp": true, "supportUdp": true}
	created := p.call("POST", "/api/v1/canvas-tunnels", body, owner, csrf)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var tunnel CanvasTunnel
	if err := json.Unmarshal(created.Body.Bytes(), &tunnel); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/canvas-tunnels", "/api/v1/canvas-tunnels/legacy-objects"} {
		if response := p.call("GET", path, nil, viewer, vcsrf); response.Code != 403 {
			t.Fatal(response.Code)
		}
	}
	if response := p.call("PUT", "/api/v1/canvas-tunnels/"+tunnel.ID, body, owner, ""); response.Code != 403 {
		t.Fatal(response.Code)
	}
	body["unknown"] = true
	if response := p.call("POST", "/api/v1/canvas-tunnels", body, owner, csrf); response.Code != 400 {
		t.Fatal(response.Code)
	}
	delete(body, "unknown")
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "a", Ref: "server:" + p.nodeA, Role: "entry"}, {InstanceID: "b", Ref: "server:" + p.nodeB, Role: "exit"}}, Connections: []CanvasConnection{{ID: "edge", Source: "a", Target: "b", TunnelID: tunnel.ID}}}
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "reference-only", Execution: "server", Layout: layout})
	if err != nil || scheme.Layout.Connections[0].Type != "tunnel" {
		t.Fatal(scheme, err)
	}
	if response := p.call("DELETE", "/api/v1/canvas-tunnels/"+tunnel.ID, nil, owner, csrf); response.Code != 409 || !strings.Contains(response.Body.String(), "tunnel_in_use") {
		t.Fatal(response.Code, response.Body.String())
	}
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "client-ref", Execution: "client", Layout: layout}); err == nil {
		t.Fatal("client accepted server reference")
	}
	if response := p.call("POST", "/api/v1/canvas-tunnels/"+tunnel.ID+"/adopt", map[string]any{"objects": []any{map[string]any{"kind": "unknown", "id": "bad"}}}, owner, csrf); response.Code != 400 {
		t.Fatal(response.Code, response.Body.String())
	}
	if len(p.audit.actions) != 1 {
		t.Fatal(p.audit.actions)
	}
	legacy, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "[orphan] API 中转入口", NodeID: p.nodeB, Protocol: "trojan", Listen: "0.0.0.0", Port: 8443, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	objects := map[string]any{"objects": []any{map[string]any{"kind": "inbound", "id": legacy.ID}}}
	path := "/api/v1/canvas-tunnels/" + tunnel.ID + "/adopt"
	if response := p.call("POST", path, objects, viewer, vcsrf); response.Code != 403 {
		t.Fatal(response.Code)
	}
	if response := p.call("POST", path, objects, owner, ""); response.Code != 403 {
		t.Fatal(response.Code)
	}
	if response := p.call("POST", path, objects, owner, csrf); response.Code != 204 {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := p.call("POST", path, objects, owner, csrf); response.Code != 409 {
		t.Fatal(response.Code, response.Body.String())
	}
	if len(p.audit.actions) != 2 || p.audit.actions[1] != "认领旧画布对象" {
		t.Fatal(p.audit.actions)
	}
}

func TestCanvasK4EndpointEditRetargetsBothDrawings(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	a, _, err := p.publishScheme(t, "endpoints-a", layout)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "endpoints-b", Execution: "server", Layout: a.Layout})
	if err != nil {
		t.Fatal(err)
	}
	tunnelID := a.Layout.Connections[0].TunnelID
	tunnel, err := p.service.Repository.canvasTunnel(p.ctx, tunnelID)
	if err != nil {
		t.Fatal(err)
	}
	input := tunnel.CanvasTunnelInput
	input.Target = "server:" + p.nodeC
	if _, err := p.service.SaveCanvasTunnel(p.ctx, tunnelID, input); err != nil {
		t.Fatal(err)
	}
	for _, schemeID := range []string{a.ID, b.ID} {
		scheme, err := p.service.Repository.CanvasScheme(p.ctx, schemeID)
		if err != nil {
			t.Fatal(err)
		}
		c := scheme.Layout.Connections[0]
		found := false
		for _, i := range scheme.Layout.Instances {
			if i.InstanceID == c.Target && i.Ref == input.Target {
				found = true
			}
		}
		if !found {
			t.Fatal(scheme)
		}
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, b.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	objects, err := p.service.Repository.tunnelObjects(p.ctx, tunnelID)
	if err != nil || len(objects) != 2 {
		t.Fatal(objects, err)
	}
	for _, o := range objects {
		if o.Kind == "inbound" && o.NodeID != p.nodeC {
			t.Fatal(o)
		}
	}
}

func TestCanvasK4SharedHopRebuildsOtherSchemeChain(t *testing.T) {
	p := newPublishPanel(t)
	a, _, err := p.publishScheme(t, "chain-a", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)}))
	if err != nil {
		t.Fatal(err)
	}
	layout := a.Layout
	layout.Instances = append([]CanvasInstance(nil), layout.Instances[:3]...)
	layout.Connections = append([]CanvasConnection(nil), layout.Connections[:1]...)
	b, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "chain-b", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	tunnelID := a.Layout.Connections[0].TunnelID
	tunnel, err := p.service.Repository.canvasTunnel(p.ctx, tunnelID)
	if err != nil {
		t.Fatal(err)
	}
	input := tunnel.CanvasTunnelInput
	input.TunnelPort = canvasTestPort(10443)
	if _, err := p.service.SaveCanvasTunnel(p.ctx, tunnelID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, b.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	entry, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := p.service.Repository.Chain(p.ctx, strings.TrimPrefix(entry.DefaultOutbound, "chain:"))
	if err != nil {
		t.Fatal(err)
	}
	if len(chain.OutboundIDs) != 2 {
		t.Fatal(chain)
	}
	out, err := p.service.Repository.Outbound(p.ctx, chain.OutboundIDs[0], true)
	if err != nil {
		t.Fatal(err)
	}
	in, err := p.service.Repository.Inbound(p.ctx, out.TargetInboundID, true)
	if err != nil || in.Port != 10443 {
		t.Fatal(in, err)
	}
}

func TestCanvasK4NoopDoesNotIgnoreManualObjectChanges(t *testing.T) {
	p := newPublishPanel(t)
	a, _, err := p.publishScheme(t, "manual-change", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}))
	if err != nil {
		t.Fatal(err)
	}
	objects, err := p.service.Repository.tunnelObjects(p.ctx, a.Layout.Connections[0].TunnelID)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if object.Kind == "inbound" {
			if _, err := p.database.Exec("UPDATE inbounds SET enabled=0 WHERE id=?", object.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	before := p.deployer.calls
	if _, err := p.service.PublishCanvasScheme(p.ctx, a.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if p.deployer.calls != before+1 {
		t.Fatal("manual change incorrectly treated as a no-op")
	}
}

func TestCanvasK4SharedPublishLeavesDisconnectedDraftAlone(t *testing.T) {
	p := newPublishPanel(t)
	a, _, err := p.publishScheme(t, "disconnected-a", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "disconnected-b", Execution: "server", Layout: a.Layout})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Layout.Instances = append(stored.Layout.Instances, CanvasInstance{InstanceID: "c", Ref: "server:" + p.nodeC, Role: "entry"}, CanvasInstance{InstanceID: "d", Ref: "server:" + p.nodeC, Role: "exit"})
	stored.Layout.Connections = append(stored.Layout.Connections, CanvasConnection{ID: "incomplete-forward", Source: "c", Target: "d", Type: "port_forward", SupportTCP: true})
	if _, err := p.service.SaveCanvasScheme(p.ctx, a.ID, CanvasSchemeInput{Name: a.Name, Execution: "server", Layout: stored.Layout, UpdatedAt: stored.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	before := p.deployer.calls
	if _, err := p.service.PublishCanvasScheme(p.ctx, b.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if p.deployer.calls != before {
		t.Fatal("disconnected draft entered the shared publication")
	}
}

func TestCanvasK4ArchiveValidationRejectsDanglingOwnershipAndUnknownJSON(t *testing.T) {
	p := newPublishPanel(t)
	a, _, err := p.publishScheme(t, "archive-integrity", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}))
	if err != nil {
		t.Fatal(err)
	}
	tunnelID := a.Layout.Connections[0].TunnelID
	if err := ValidateCanvasTunnels(p.ctx, p.database); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("INSERT INTO canvas_tunnel_objects VALUES(?,'inbound','missing-object')", tunnelID); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCanvasTunnels(p.ctx, p.database); err == nil {
		t.Fatal("dangling ownership accepted")
	}
	if _, err := p.database.Exec("DELETE FROM canvas_tunnel_objects WHERE object_id='missing-object'"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("UPDATE canvas_tunnels SET connection_json=json_set(connection_json,'$.unknown',1) WHERE id=?", tunnelID); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCanvasTunnels(p.ctx, p.database); err == nil {
		t.Fatal("unknown tunnel JSON accepted")
	}
}

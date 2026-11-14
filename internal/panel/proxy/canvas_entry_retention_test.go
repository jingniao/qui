package proxy

import (
	"proxycontrol/internal/panel/node"
	"strings"
	"testing"
)

func TestCanvasManagedEntryMissingPathKeepsRoute(t *testing.T) {
	for _, removeInstance := range []bool{false, true} {
		t.Run(map[bool]string{false: "entry retained", true: "entry removed"}[removeInstance], func(t *testing.T) {
			p := newPublishPanel(t)
			layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)})
			scheme := savePlanScheme(t, p, layout)
			if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
				t.Fatal(err)
			}
			before, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.service.Repository.InsertCanvasEntryBinding(p.ctx, CanvasEntryBindingRecord{ID: "managed-entry", SchemeID: &scheme.ID, InstanceID: "i-in", InboundID: before.ID, Mode: "reuse", OriginalDefaultOutbound: "direct", AppliedDefaultOutbound: before.DefaultOutbound, State: "applied"}); err != nil {
				t.Fatal(err)
			}
			layout.Connections = nil
			if removeInstance {
				layout.Instances = layout.Instances[:3]
			}
			scheme, err = p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
			if err != nil {
				t.Fatal(err)
			}
			scheme, err = p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: scheme.Name, Execution: "server", Layout: layout, UpdatedAt: scheme.UpdatedAt})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
			if err != nil || !preview.OK || !strings.Contains(strings.Join(preview.Warnings, ";"), "保持") {
				t.Fatalf("preview %+v, %v", preview, err)
			}
			for _, server := range preview.Servers {
				if len(server.Removes) != 0 {
					t.Fatalf("managed route removed: %+v", server)
				}
			}
			result, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
			if err != nil || !strings.Contains(strings.Join(result.Warnings, ";"), "保持") {
				t.Fatalf("publication %+v, %v", result, err)
			}
			after, err := p.service.Repository.Inbound(p.ctx, before.ID, true)
			if err != nil || after.DefaultOutbound != before.DefaultOutbound {
				t.Fatalf("route %q -> %q, %v", before.DefaultOutbound, after.DefaultOutbound, err)
			}
			inbounds, outbounds, chains := p.publishedObjects(t, scheme.Name)
			if len(inbounds) != 2 || len(outbounds) != 2 || len(chains) != 1 {
				t.Fatalf("retained objects: %d %d %d", len(inbounds), len(outbounds), len(chains))
			}
		})
	}
}

func TestCanvasExistingEntryPreviewUsesCurrentGroups(t *testing.T) {
	p := newPublishPanel(t)
	p.service.EntryGroups = p.service.Nodes.(*node.Repository)
	entryPlanGroup(t, p, "first", "mixed", p.inbound.ID)
	entryPlanGroup(t, p, "second", "mixed", p.inbound.ID)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "explicit", GroupIDs: []string{"first"}})
	scheme := savePlanScheme(t, p, layout)
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	layout.Instances[0].EntryBinding.GroupIDs = []string{"second"}
	preview, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, &layout)
	if err != nil || !preview.OK || len(preview.Entries) != 1 || len(preview.Entries[0].Groups) != 1 || preview.Entries[0].Groups[0].ID != "first" {
		t.Fatalf("preview %+v, %v", preview, err)
	}
	for _, server := range preview.Servers {
		for _, created := range server.Creates {
			if created.Name == "Node A → Node B" {
				t.Fatal("existing entry planned as create")
			}
		}
	}
}

func TestCanvasBoundEntryMissingPathKeepsRoute(t *testing.T) {
	for _, mode := range []string{"create", "reuse"} {
		t.Run(mode, func(t *testing.T) {
			p := newPublishPanel(t)
			binding := &CanvasEntryBinding{Mode: mode, Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
			if mode == "reuse" {
				binding.InboundID = p.inbound.ID
			}
			scheme := savePlanScheme(t, p, entryPlanLayout(p, binding))
			result, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
			if err != nil {
				t.Fatal(err)
			}
			before := result.Entries[0]
			scheme, err = p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
			if err != nil {
				t.Fatal(err)
			}
			scheme.Layout.Connections = nil
			scheme, err = p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: scheme.Name, Execution: "server", Layout: scheme.Layout, UpdatedAt: scheme.UpdatedAt})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
			if err != nil || !preview.OK || len(preview.Entries) != 1 || preview.Entries[0].Route != before.Route || !strings.Contains(strings.Join(preview.Warnings, ";"), "保持") {
				t.Fatalf("preview %+v, %v", preview, err)
			}
			for range 2 {
				result, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
				if err != nil || len(result.Entries) != 1 || result.Entries[0].InboundID != before.InboundID || result.Entries[0].Route != before.Route || !strings.Contains(strings.Join(result.Warnings, ";"), "保持") {
					t.Fatalf("publication %+v, %v", result, err)
				}
			}
		})
	}
}

package proxy

import (
	"strings"
	"testing"
)

func TestCanvasEntrySharedPublishPreservesBindingOwner(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)})
	layout.Instances[0].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
	a, _, err := p.publishScheme(t, "managed-a", layout)
	if err != nil {
		t.Fatal(err)
	}
	before, err := p.service.Repository.CanvasEntryBinding(p.ctx, a.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	layout = a.Layout
	layout.Instances = append([]CanvasInstance{}, layout.Instances[:2]...)
	layout.Instances[0].EntryBinding = nil
	layout.Connections = append([]CanvasConnection{}, layout.Connections[:1]...)
	b, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "managed-b", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	tunnel, err := p.service.Repository.canvasTunnel(p.ctx, a.Layout.Connections[0].TunnelID)
	if err != nil {
		t.Fatal(err)
	}
	input := tunnel.CanvasTunnelInput
	input.TunnelPort = canvasTestPort(10443)
	if _, err := p.service.SaveCanvasTunnel(p.ctx, tunnel.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, b.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	after, err := p.service.Repository.CanvasEntryBinding(p.ctx, a.ID, "i-a")
	if err != nil || after.ID != before.ID || after.InboundID != before.InboundID || after.SchemeID == nil || *after.SchemeID != a.ID {
		t.Fatalf("binding before %+v after %+v: %v", before, after, err)
	}
	entry, err := p.service.Repository.Inbound(p.ctx, after.InboundID, true)
	if err != nil || entry.DefaultOutbound != after.AppliedDefaultOutbound || !strings.HasPrefix(entry.DefaultOutbound, "chain:") {
		t.Fatalf("entry %+v: %v", entry, err)
	}
	chain, err := p.service.Repository.Chain(p.ctx, strings.TrimPrefix(entry.DefaultOutbound, "chain:"))
	if err != nil {
		t.Fatal(err)
	}
	outbound, err := p.service.Repository.Outbound(p.ctx, chain.OutboundIDs[0], true)
	if err != nil {
		t.Fatal(err)
	}
	relay, err := p.service.Repository.Inbound(p.ctx, outbound.TargetInboundID, true)
	if err != nil || relay.Port != 10443 {
		t.Fatalf("shared relay %+v: %v", relay, err)
	}
	var count int
	if err := p.database.QueryRow("SELECT COUNT(*) FROM canvas_entry_bindings").Scan(&count); err != nil || count != 1 {
		t.Fatalf("bindings %d: %v", count, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, b.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasEntryRepeatRejectsConfigurationChanges(t *testing.T) {
	for _, change := range []string{"protocol", "core", "port"} {
		t.Run(change, func(t *testing.T) {
			p := newPublishPanel(t)
			layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})
			scheme, _, err := p.publishScheme(t, "changed", layout)
			if err != nil {
				t.Fatal(err)
			}
			binding := scheme.Layout.Instances[0].EntryBinding
			switch change {
			case "protocol":
				binding.Protocol = "trojan"
			case "core":
				binding.Core = "xray"
			case "port":
				binding.Port = canvasTestPort(20001)
			}
			result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, &scheme.Layout)
			if err != nil || result.OK {
				t.Fatalf("changed entry accepted %+v: %v", result, err)
			}
		})
	}
}

func TestCanvasEntryRepeatPreviewKeepsActualGroups(t *testing.T) {
	p := newPublishPanel(t)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})
	scheme, _, err := p.publishScheme(t, "groups", layout)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	entryPlanGroup(t, p, "actual", "mixed", binding.InboundID)
	entryPlanGroup(t, p, "requested", "subscription")
	scheme.Layout.Instances[0].EntryBinding.GroupMode = "explicit"
	scheme.Layout.Instances[0].EntryBinding.GroupIDs = []string{"requested"}
	result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, &scheme.Layout)
	if err != nil || !result.OK || len(result.Entries) != 1 || len(result.Entries[0].Groups) != 1 || result.Entries[0].Groups[0].ID != "actual" {
		t.Fatalf("preview %+v: %v", result, err)
	}
}

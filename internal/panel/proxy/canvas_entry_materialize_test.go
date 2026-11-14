package proxy

import (
	"proxycontrol/internal/panel/node"
	"reflect"
	"strings"
	"testing"
)

func TestCanvasEntryMaterializeCreateAndRepeat(t *testing.T) {
	p := newPublishPanel(t)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})
	scheme := savePlanScheme(t, p, layout)
	for range 2 {
		plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.service.canvasApplyPlan(p.ctx, plan); err != nil {
			t.Fatal(err)
		}
		binding, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
		if err != nil {
			t.Fatal(err)
		}
		inbound, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, true)
		if err != nil {
			t.Fatal(err)
		}
		if inbound.Name != "Node A → Node B" || inbound.Protocol != "socks5" || inbound.Port != 20000 || inbound.InternalRelay || !inbound.SubscriptionVisible || inbound.DefaultOutbound == "direct" || inbound.DefaultOutbound != binding.AppliedDefaultOutbound {
			t.Fatalf("inbound %+v binding %+v", inbound, binding)
		}
		removal, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
		if err != nil || !removal.OK || len(removal.Entries[0].Changes) != 0 {
			t.Fatalf("advanced create baseline: %v ok=%v", err, removal.OK)
		}
	}
	var count int
	if err := p.database.QueryRow("SELECT COUNT(*) FROM inbounds WHERE name='Node A → Node B'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("count %d: %v", count, err)
	}
}

func TestCanvasEntryMaterializeReuseOriginal(t *testing.T) {
	p := newPublishPanel(t)
	entryPlanGroup(t, p, "existing", "mixed", p.inbound.ID)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"})
	scheme := savePlanScheme(t, p, layout)
	for round := range 2 {
		plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.service.canvasApplyPlan(p.ctx, plan); err != nil {
			t.Fatal(err)
		}
		binding, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
		if err != nil {
			t.Fatal(err)
		}
		if binding.OriginalDefaultOutbound != "direct" || binding.AppliedDefaultOutbound == "direct" {
			t.Fatalf("round %d binding %+v", round, binding)
		}
		groups, err := p.service.canvasEntryGroups(p.ctx, p.inbound.ID, false)
		if err != nil || len(groups) != 1 || groups[0].ID != "existing" {
			t.Fatalf("reuse groups %+v: %v", groups, err)
		}
	}
}

func TestCanvasEntryMaterializeAutoPortAndChain(t *testing.T) {
	p := newPublishPanel(t)
	entryPlanRange(t, p, p.nodeA, 20000, 20000)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "auto", GroupMode: "none"})
	layout.Instances[1].Role = "relay"
	layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "i-c", Ref: "server:" + p.nodeC, Role: "exit"})
	layout.Connections = append(layout.Connections, canvasTestHop("i-b", "i-c", 9443))
	scheme := savePlanScheme(t, p, layout)
	for range 2 {
		plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.service.canvasApplyPlan(p.ctx, plan); err != nil {
			t.Fatal(err)
		}
		record, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
		if err != nil {
			t.Fatal(err)
		}
		inbound, err := p.service.Repository.Inbound(p.ctx, record.InboundID, true)
		if err != nil || inbound.Port != 20000 || inbound.Name != "Node A → Node B → Node C" || !strings.HasPrefix(inbound.DefaultOutbound, "chain:") {
			t.Fatalf("chain entry %+v: %v", inbound, err)
		}
	}
}

func TestCanvasEntryMaterializeGroups(t *testing.T) {
	for _, mode := range []string{"exit_groups", "explicit", "none"} {
		t.Run(mode, func(t *testing.T) {
			p := newPublishPanel(t)
			p.service.EntryGroups = p.service.Nodes.(*node.Repository)
			exit, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "Exit", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
			if err != nil {
				t.Fatal(err)
			}
			entryPlanGroup(t, p, "exit", "subscription", exit.ID)
			entryPlanGroup(t, p, "selected", "mixed", p.inbound.ID)
			binding := &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: mode}
			want := []string{}
			if mode == "exit_groups" {
				want = []string{"exit"}
			}
			if mode == "explicit" {
				binding.GroupIDs = []string{"selected"}
				want = []string{"selected"}
			}
			layout := entryPlanLayout(p, binding)
			layout.Instances[1].Ref = "inbound:" + exit.ID
			scheme := savePlanScheme(t, p, layout)
			plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.service.canvasApplyPlan(p.ctx, plan); err != nil {
				t.Fatal(err)
			}
			record, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
			if err != nil {
				t.Fatal(err)
			}
			groups, err := p.service.canvasEntryGroups(p.ctx, record.InboundID, false)
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, g := range groups {
				got = append(got, g.ID)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("groups %v want %v", got, want)
			}
		})
	}
}

func TestCanvasEntryMaterializeFailureUndo(t *testing.T) {
	p := newPublishPanel(t)
	p.service.EntryGroups = p.service.Nodes.(*node.Repository)
	entryPlanGroup(t, p, "selected", "mixed", p.inbound.ID)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"})
	layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "new", Ref: "server:" + p.nodeA, Role: "entry", EntryBinding: &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "explicit", GroupIDs: []string{"selected"}}})
	scheme := savePlanScheme(t, p, layout)
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("CREATE TRIGGER fail_entry_port BEFORE UPDATE OF allocated_port ON canvas_tunnels BEGIN SELECT RAISE(ABORT, 'forced entry failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.canvasApplyPlan(p.ctx, plan); err == nil || !strings.Contains(err.Error(), "forced entry failure") {
		t.Fatalf("want injected failure, got %v", err)
	}
	inbound, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, true)
	if err != nil || inbound.DefaultOutbound != "direct" {
		t.Fatalf("restored %+v %v", inbound, err)
	}
	for _, table := range []string{"canvas_entry_bindings", "outbounds", "proxy_chains"} {
		var count int
		if err := p.database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count %d: %v", table, count, err)
		}
	}
	var count int
	if err := p.database.QueryRow("SELECT COUNT(*) FROM inbounds").Scan(&count); err != nil || count != 1 {
		t.Fatalf("inbounds count %d: %v", count, err)
	}
	group, err := p.service.EntryGroups.Group(p.ctx, "selected")
	if err != nil || !reflect.DeepEqual(group.InboundIDs, []string{p.inbound.ID}) {
		t.Fatalf("group %+v: %v", group, err)
	}
}

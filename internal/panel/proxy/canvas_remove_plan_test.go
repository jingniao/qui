package proxy

import (
	"errors"
	"strings"
	"testing"
)

func TestCanvasRemovePlanReadOnlyAndOriginalRoute(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	build, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, build); err != nil {
		t.Fatal(err)
	}
	p.database.SetMaxOpenConns(1)
	before := quickCanvasChanges(t, p)
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || len(plan.Entries) != 1 || plan.Entries[0].RestoreTarget != "direct" || len(plan.Objects) != 2 || plan.Nodes[0] != p.nodeA {
		t.Fatalf("plan %+v", plan)
	}
	if after := quickCanvasChanges(t, p); after != before {
		t.Fatalf("read-only plan wrote %d changes", after-before)
	}
}

func TestCanvasRemovePlanCreateRequiresConfigAndGroupConfirmation(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}))
	build, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, build); err != nil {
		t.Fatal(err)
	}
	b, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	entryPlanGroup(t, p, "manual-group", "mixed", b.InboundID)
	if _, err = p.database.Exec("UPDATE inbounds SET default_outbound='block',port=20001 WHERE id=?", b.InboundID); err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if plan.OK || len(plan.Entries[0].Groups) != 1 || len(plan.Entries[0].Changes) != 3 || !strings.Contains(strings.Join(plan.Entries[0].Changes, " "), "manual-group") {
		t.Fatalf("plan %+v", plan)
	}
	plan, err = p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true})
	if err != nil || !plan.OK {
		t.Fatalf("acknowledged %+v: %v", plan, err)
	}
}

func TestCanvasRemovePlanRetainsExternalReferenceDependencies(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	build, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, build); err != nil {
		t.Fatal(err)
	}
	b, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "Other user entry", NodeID: p.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 20001, Enabled: true, SubscriptionVisible: true, DefaultOutbound: b.AppliedDefaultOutbound})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || len(plan.Objects) != 2 {
		t.Fatalf("plan %+v", plan)
	}
	found := false
	for _, o := range plan.Objects {
		if !o.Keep {
			t.Fatalf("deleted referenced object %+v", o)
		}
		for _, ref := range o.Referrers {
			if ref.ID == other.ID {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing external referrer")
	}
}

func TestCanvasRemovePlanLegacyEntryNeedsExplicitTarget(t *testing.T) {
	p := newPublishPanel(t)
	layout := entryPlanLayout(p, nil)
	layout.Instances[0].Ref = "inbound:" + p.inbound.ID
	scheme := savePlanScheme(t, p, layout)
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if plan.OK || len(plan.Entries) != 1 || plan.Entries[0].BindingID != "legacy:i-a" || !plan.Entries[0].NeedsRestoreTarget {
		t.Fatalf("legacy %+v", plan)
	}
	plan, err = p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, RestoreTargets: map[string]string{"legacy:i-a": "direct"}})
	if err != nil || !plan.OK {
		t.Fatalf("explicit %+v: %v", plan, err)
	}
}

func TestCanvasRemovePlanRejectsCyclicRestoreTarget(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	build, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, build); err != nil {
		t.Fatal(err)
	}
	b, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a historical route whose relay target has subsequently been edited.
	if _, err = p.database.Exec("UPDATE outbounds SET target_inbound_id=? WHERE id=?", p.inbound.ID, b.AppliedDefaultOutbound); err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, RestoreTargets: map[string]string{b.ID: b.AppliedDefaultOutbound}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.OK || !plan.Entries[0].NeedsRestoreTarget || !strings.Contains(plan.Entries[0].RestoreReason, "环路") {
		t.Fatalf("cycle %+v", plan)
	}
}

func TestCanvasRemovePlanMissingOriginalAndStale(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	build, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, build); err != nil {
		t.Fatal(err)
	}
	b, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("UPDATE canvas_entry_bindings SET original_default_outbound='missing-original' WHERE id=?", b.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if plan.OK || !plan.Entries[0].NeedsRestoreTarget {
		t.Fatalf("plan %+v", plan)
	}
	plan, err = p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, RestoreTargets: map[string]string{b.ID: "block"}})
	if err != nil || !plan.OK {
		t.Fatalf("plan %+v: %v", plan, err)
	}
	if _, err = p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: "stale"}); !errors.Is(err, ErrStaleScheme) {
		t.Fatalf("stale: %v", err)
	}
}

func TestCanvasRemovePlanRetainsSharedPublishedTunnel(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	build, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, build); err != nil {
		t.Fatal(err)
	}
	other, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "Other published scheme", Execution: "server", Layout: CanvasLayout{Instances: []CanvasInstance{}, Connections: []CanvasConnection{}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.service.Repository.markTunnelPublication(p.ctx, other.ID, "", build); err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || len(plan.Objects) != 2 {
		t.Fatalf("plan %+v", plan)
	}
	for _, o := range plan.Objects {
		if !o.Keep || len(o.Referrers) != 1 || o.Referrers[0].ID != other.ID || o.Referrers[0].Kind != "scheme" {
			t.Fatalf("shared %+v", o)
		}
	}
}

func TestCanvasRemovePlanChangedReuseRequiresExplicitTarget(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	build, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, build); err != nil {
		t.Fatal(err)
	}
	b, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("UPDATE inbounds SET default_outbound='block' WHERE id=?", p.inbound.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.OK || !plan.Entries[0].NeedsRestoreTarget {
		t.Fatalf("changed %+v", plan)
	}
	plan, err = p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, RestoreTargets: map[string]string{b.ID: "direct"}})
	if err != nil || !plan.OK {
		t.Fatalf("explicit %+v %v", plan, err)
	}
}

func TestCanvasRemovePlanValidatesCombinedRestoreRoutes(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	second, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "Second entry", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 20001, Enabled: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	entries := []Inbound{p.inbound, second}
	targets := map[string]string{}
	for i, entry := range entries {
		key := []string{"binding-a", "binding-b"}[i]
		if err = p.service.Repository.InsertCanvasEntryBinding(p.ctx, CanvasEntryBindingRecord{ID: key, SchemeID: &scheme.ID, InstanceID: key, InboundID: entry.ID, Mode: "reuse", OriginalDefaultOutbound: "direct", AppliedDefaultOutbound: "direct", State: "applied"}); err != nil {
			t.Fatal(err)
		}
		out, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: key, NodeID: entry.NodeID, Protocol: "direct", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = p.database.Exec("UPDATE outbounds SET target_inbound_id=?,target_node_id=? WHERE id=?", entries[1-i].ID, entries[1-i].NodeID, out.ID); err != nil {
			t.Fatal(err)
		}
		targets[key] = out.ID
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, RestoreTargets: targets})
	if err != nil {
		t.Fatal(err)
	}
	if plan.OK {
		t.Fatalf("combined restore accepted cycle: %+v", plan)
	}
	for _, entry := range plan.Entries {
		if !entry.NeedsRestoreTarget || !strings.Contains(entry.RestoreReason, "环路") {
			t.Fatalf("entry %+v", entry)
		}
	}
}

func TestCanvasRemovePlanBackupChainRestore(t *testing.T) {
	p := newPublishPanel(t)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"})
	layout.Instances[1].Role = "relay"
	layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "i-c", Ref: "server:" + p.nodeC, Role: "exit"})
	layout.Connections = append(layout.Connections, canvasTestHop("i-b", "i-c", 9443))
	scheme := savePlanScheme(t, p, layout)
	build, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, build); err != nil {
		t.Fatal(err)
	}
	b, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	base, err := p.service.Repository.Chain(p.ctx, strings.TrimPrefix(b.AppliedDefaultOutbound, "chain:"))
	if err != nil {
		t.Fatal(err)
	}
	backupLayout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20002), GroupMode: "none"})
	backupLayout.Instances[1].Role = "relay"
	backupLayout.Instances = append(backupLayout.Instances, CanvasInstance{InstanceID: "i-c", Ref: "server:" + p.nodeC, Role: "exit"})
	backupLayout.Connections = []CanvasConnection{canvasTestHop("i-a", "i-b", 8444), canvasTestHop("i-b", "i-c", 9444)}
	backupScheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "Backup scheme", Execution: "server", Layout: backupLayout})
	if err != nil {
		t.Fatal(err)
	}
	backupPlan, err := p.service.canvasBuildPlan(p.ctx, backupScheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, backupPlan); err != nil {
		t.Fatal(err)
	}
	backupBinding, err := p.service.Repository.CanvasEntryBinding(p.ctx, backupScheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	backupID := strings.TrimPrefix(backupBinding.AppliedDefaultOutbound, "chain:")
	primary, err := p.service.SaveChain(p.ctx, base.ID, ChainInput{Name: base.Name, NodeIDs: base.NodeIDs, OutboundIDs: base.OutboundIDs, FinalOutboundID: "direct", SupportTCP: base.SupportTCP, SupportUDP: base.SupportUDP, Enabled: true, FailurePolicy: "BackupChain", BackupTargetID: backupID})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, RestoreTargets: map[string]string{b.ID: "chain:" + primary.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || plan.Entries[0].NeedsRestoreTarget {
		t.Fatalf("valid backup chain rejected %+v", plan)
	}
}

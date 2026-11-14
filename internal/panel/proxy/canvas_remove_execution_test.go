package proxy

import (
	"errors"
	"testing"
)

func TestCanvasRemoveMissingOriginalRequiresExplicitTargetThenSucceeds(t *testing.T) {
	p, scheme, binding, deploy := removeFixture(t, "reuse")
	if _, err := p.database.Exec("UPDATE canvas_entry_bindings SET original_default_outbound='missing-outbound' WHERE id=?", binding.ID); err != nil {
		t.Fatal(err)
	}
	_, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt}, "owner", p.users)
	var conflict *CanvasRemoveConflict
	if !errors.As(err, &conflict) || !conflict.Plan.Entries[0].NeedsRestoreTarget || len(deploy.phases) != 0 {
		t.Fatalf("expected explicit target conflict: %v", err)
	}
	_, err = p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, RestoreTargets: map[string]string{binding.ID: "block"}}, "owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, false)
	if err != nil || inbound.DefaultOutbound != "block" {
		t.Fatalf("restored %+v: %v", inbound, err)
	}
}

func TestCanvasRemoveKeepsSharedChannelAndReferrers(t *testing.T) {
	p, scheme, _, deploy := removeFixture(t, "create")
	layout := scheme.Layout
	layout.Instances = append([]CanvasInstance{}, layout.Instances...)
	layout.Instances[0].EntryBinding = nil
	other, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "Shared channel", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Objects) == 0 {
		t.Fatal("missing channel objects")
	}
	for _, object := range plan.Objects {
		found := false
		for _, ref := range object.Referrers {
			found = found || ref.Kind == "scheme" && ref.ID == other.ID
		}
		if !object.Keep || !found {
			t.Fatalf("shared reference missing: %+v", object)
		}
	}
	if _, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users); err != nil {
		t.Fatal(err)
	}
	if len(deploy.phases) != 1 {
		t.Fatalf("shared channels must not stop: %d phases", len(deploy.phases))
	}
	if _, err := p.service.Repository.canvasTunnel(p.ctx, scheme.Layout.Connections[0].TunnelID); err != nil {
		t.Fatalf("shared tunnel deleted: %v", err)
	}
	for _, object := range plan.Objects {
		var enabled bool
		if err := p.database.QueryRow("SELECT enabled FROM "+canvasRemoveTable(object.Kind)+" WHERE id=?", object.ID).Scan(&enabled); err != nil || !enabled {
			t.Fatalf("shared object %+v: enabled=%v err=%v", object, enabled, err)
		}
	}
	if _, err := p.service.Repository.CanvasScheme(p.ctx, other.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasRemoveRejectsNewEntryReferenceDuringPublication(t *testing.T) {
	p, scheme, binding, deploy := removeFixture(t, "create")
	deploy.afterPublish = func(phase int) {
		if phase == 2 {
			if _, err := p.database.Exec("UPDATE outbounds SET target_inbound_id=?,target_node_id=? WHERE id=?", binding.InboundID, p.nodeA, binding.AppliedDefaultOutbound); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users)
	if err == nil {
		t.Fatal("new entry reference was ignored")
	}
	if _, readErr := p.service.Repository.Inbound(p.ctx, binding.InboundID, false); readErr != nil {
		t.Fatalf("entry deleted after new reference: %v", readErr)
	}
}

func TestCanvasRemoveFinalTransactionRejectsChangedObject(t *testing.T) {
	p, scheme, binding, _ := removeFixture(t, "create")
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true})
	if err != nil {
		t.Fatal(err)
	}
	guards, err := p.service.canvasRemovalRows(p.ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("UPDATE inbounds SET port=20002 WHERE id=?", binding.InboundID); err != nil {
		t.Fatal(err)
	}
	if err := p.service.finishCanvasRemoval(p.ctx, plan, guards); err == nil {
		t.Fatal("changed entry was deleted")
	}
	inbound, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, false)
	if err != nil || inbound.Port != 20002 {
		t.Fatalf("concurrent edit lost: %+v %v", inbound, err)
	}
	if _, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasRemoveFinalTransactionRejectsNewChannelReference(t *testing.T) {
	p, scheme, binding, _ := removeFixture(t, "create")
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true})
	if err != nil {
		t.Fatal(err)
	}
	guards, err := p.service.canvasRemovalRows(p.ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("UPDATE inbounds SET default_outbound=? WHERE id=?", binding.AppliedDefaultOutbound, p.inbound.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.service.finishCanvasRemoval(p.ctx, plan, guards); err == nil {
		t.Fatal("new channel reference was deleted")
	}
	if _, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasRemoveFinalTransactionRejectsNewSchemeReference(t *testing.T) {
	p, scheme, _, _ := removeFixture(t, "create")
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true})
	if err != nil {
		t.Fatal(err)
	}
	guards, err := p.service.canvasRemovalRows(p.ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	layout := scheme.Layout
	layout.Instances = append([]CanvasInstance{}, layout.Instances...)
	layout.Instances[0].EntryBinding = nil
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "New shared channel", Execution: "server", Layout: layout}); err != nil {
		t.Fatal(err)
	}
	if err := p.service.finishCanvasRemoval(p.ctx, plan, guards); err == nil {
		t.Fatal("new scheme reference was ignored")
	}
	if _, err := p.service.Repository.canvasTunnel(p.ctx, scheme.Layout.Connections[0].TunnelID); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasRemoveKeepsPublishedOnlySharedTunnel(t *testing.T) {
	p, scheme, _, _ := removeFixture(t, "create")
	layout := scheme.Layout
	layout.Instances = append([]CanvasInstance{}, layout.Instances...)
	layout.Instances[0].EntryBinding = nil
	other, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "Published shared channel", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	tunnelID := scheme.Layout.Connections[0].TunnelID
	if _, err := p.database.Exec("INSERT INTO canvas_tunnel_publications VALUES(?,?)", other.ID, tunnelID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("UPDATE canvas_schemes SET layout_json=json_set(layout_json,'$.connections',json('[]')) WHERE id=?", other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.canvasTunnel(p.ctx, tunnelID); err != nil {
		t.Fatal(err)
	}
	objects, err := p.service.Repository.tunnelObjects(p.ctx, tunnelID)
	if err != nil || len(objects) != 2 {
		t.Fatalf("shared objects %+v %v", objects, err)
	}
}

package proxy

import (
	"encoding/json"
	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"
	"testing"
)

// Reconstruct a pre-0124 database retaining published object IDs and credentials.
func canvasK4LegacyDatabase(t *testing.T, p *publishPanel, scheme CanvasScheme, deleted bool) {
	t.Helper()
	rewindPostK4Fixture(t, p.database)
	layout := scheme.Layout
	for n := range layout.Connections {
		layout.Connections[n].TunnelID = ""
	}
	raw, err := json.Marshal(layout)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DROP TRIGGER canvas_tunnel_inbound_deleted", "DROP TRIGGER canvas_tunnel_outbound_deleted", "DROP TRIGGER canvas_tunnel_chain_deleted", "DROP TABLE canvas_tunnel_objects", "DROP TABLE canvas_tunnel_publications", "DROP TABLE canvas_tunnels", "DROP TABLE canvas_tunnel_reconcile", "DELETE FROM schema_migrations WHERE version='0124_canvas_tunnels.sql'"} {
		if _, err := p.database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if deleted {
		_, err = p.database.Exec("DELETE FROM canvas_schemes WHERE id=?", scheme.ID)
	} else {
		_, err = p.database.Exec("UPDATE canvas_schemes SET layout_json=? WHERE id=?", string(raw), scheme.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(p.ctx, p.database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	if err := p.service.Repository.ReconcileCanvasTunnels(p.ctx); err != nil {
		t.Fatal(err)
	}
}
func TestCanvasK4PreflightPublishedRenameKeepsOwnership(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	scheme, _, err := p.publishScheme(t, "legacy-owner", layout)
	if err != nil {
		t.Fatal(err)
	}
	canvasK4LegacyDatabase(t, p, scheme, false)
	before, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !before.OK {
		t.Fatal(before, err)
	}
	for _, server := range before.Servers {
		if len(server.Creates)+len(server.Removes) != 0 {
			t.Fatal(server)
		}
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Layout.Connections[0].TunnelID == "" {
		t.Fatal(stored)
	}
	renamed, err := p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: "renamed-owner", Execution: "server", Layout: stored.Layout, UpdatedAt: stored.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	after, err := p.service.PlanCanvasScheme(p.ctx, renamed.ID, nil)
	if err != nil || !after.OK {
		t.Fatal(after, err)
	}
	for _, server := range after.Servers {
		if len(server.Creates)+len(server.Removes) != 0 {
			t.Fatal(server)
		}
	}
	objects, err := p.service.Repository.tunnelObjects(p.ctx, stored.Layout.Connections[0].TunnelID)
	if err != nil || len(objects) != 2 {
		t.Fatal(objects, err)
	}
	if err := p.service.Repository.ReconcileCanvasTunnels(p.ctx); err != nil {
		t.Fatal(err)
	}
}
func TestCanvasK4PreflightDeletedSchemeLeavesUnownedPublication(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	scheme, _, err := p.publishScheme(t, "deleted-owner", layout)
	if err != nil {
		t.Fatal(err)
	}
	canvasK4LegacyDatabase(t, p, scheme, true)
	replacement, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: scheme.Name, Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanCanvasScheme(p.ctx, replacement.ID, nil)
	if err != nil || plan.OK {
		t.Fatal(plan, err)
	}
	found := false
	for _, c := range plan.Conflicts {
		if c.Reason == "legacy_object" && c.ObjectID != "" {
			found = true
		}
	}
	if !found {
		t.Fatal(plan)
	}
	legacy, err := p.service.Repository.LegacyCanvasObjects(p.ctx)
	if err != nil || len(legacy) != 2 {
		t.Fatal(legacy, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, replacement.ID, "Owner", p.users); err == nil {
		t.Fatal("legacy objects silently adopted")
	}
	for _, o := range legacy {
		owner, err := p.service.Repository.objectOwner(p.ctx, o.Kind, o.ID)
		if err != nil || owner != "" {
			t.Fatal(owner, err)
		}
	}
}

func TestCanvasK4PreflightRenameBeforeMigrationRemainsLegacy(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	scheme, _, err := p.publishScheme(t, "original-name", layout)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: "renamed-before-upgrade", Execution: "server", Layout: stored.Layout, UpdatedAt: stored.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	canvasK4LegacyDatabase(t, p, renamed, false)
	legacy, err := p.service.Repository.LegacyCanvasObjects(p.ctx)
	if err != nil || len(legacy) != 2 {
		t.Fatal(legacy, err)
	}
	plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || plan.OK {
		t.Fatal(plan, err)
	}
	if len(plan.Conflicts) == 0 || plan.Conflicts[0].Reason != "legacy_object" {
		t.Fatal(plan)
	}
}

func TestCanvasK4PreflightAllocatedPortRetained(t *testing.T) {
	p := newPublishPanel(t)
	if _, err := p.database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,20000,20002)", p.nodeB); err != nil {
		t.Fatal(err)
	}
	c := canvasTestHop("i-a", "i-b", 8443)
	c.TunnelPort = nil
	scheme, _, err := p.publishScheme(t, "allocated-legacy", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{c}))
	if err != nil {
		t.Fatal(err)
	}
	canvasK4LegacyDatabase(t, p, scheme, false)
	plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !plan.OK {
		t.Fatal(plan, err)
	}
	for _, server := range plan.Servers {
		if len(server.Creates)+len(server.Removes) != 0 {
			t.Fatal(server)
		}
	}
}

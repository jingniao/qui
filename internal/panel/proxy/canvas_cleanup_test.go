package proxy

import (
	"errors"
	"strings"
	"testing"
)

func cleanupFixture(t *testing.T) (*publishPanel, CanvasScheme, CanvasEntryBindingRecord) {
	t.Helper()
	p, _ := quickCanvasFixture(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}))
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.canvasApplyPlan(p.ctx, plan); err != nil {
		t.Fatal(err)
	}
	binding, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	return p, scheme, binding
}

func TestCanvasRemoveOfflineImmediatelyDeletes(t *testing.T) {
	p, scheme, binding := cleanupFixture(t)
	if _, err := p.database.Exec("UPDATE nodes SET status='offline'"); err != nil {
		t.Fatal(err)
	}
	entryPlanGroup(t, p, "cleanup-group", "mixed", binding.InboundID)
	result, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt}, "owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Removed || len(result.PendingNodes) != 2 {
		t.Fatalf("result %+v", result)
	}
	for _, table := range []string{"canvas_schemes", "canvas_entry_bindings", "canvas_tunnel_objects", "canvas_tunnels"} {
		var count int
		if err := p.database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count %d: %v", table, count, err)
		}
	}
	if _, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, false); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCanvasRemoveLegacyEntryDeletes(t *testing.T) {
	p := newPublishPanel(t)
	layout := entryPlanLayout(p, nil)
	layout.Instances[0].Ref = "inbound:" + p.inbound.ID
	scheme := savePlanScheme(t, p, layout)
	if _, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt}, "owner", p.users); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCanvasEntryReuseRejectedOnSaveAndPlan(t *testing.T) {
	p := newPublishPanel(t)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"})
	_, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "reuse", Execution: "server", Layout: layout})
	if err == nil || !strings.Contains(err.Error(), "新建") {
		t.Fatalf("save %v", err)
	}
	plan, err := p.service.PlanCanvas(p.ctx, CanvasScheme{Name: "reuse", Execution: "server", Layout: layout})
	if err != nil || plan.OK || !strings.Contains(plan.Message, "新建") {
		t.Fatalf("plan %+v %v", plan, err)
	}
}

func TestRelayCleanupResidualRunsOnce(t *testing.T) {
	p, scheme, binding := cleanupFixture(t)
	if err := p.service.Repository.DeleteCanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.service.ReconcileRelayCleanup(p.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, false); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	var count int
	if err := p.database.QueryRow("SELECT count(*) FROM canvas_tunnels").Scan(&count); err != nil || count != 0 {
		t.Fatalf("tunnels %d %v", count, err)
	}
	if err := p.service.Repository.InsertCanvasEntryBinding(p.ctx, CanvasEntryBindingRecord{ID: "later-detached", InboundID: p.inbound.ID, InstanceID: "later", Mode: "create", State: "detached"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("CREATE TRIGGER reject_second_cleanup BEFORE DELETE ON canvas_entry_bindings BEGIN SELECT RAISE(ABORT,'second cleanup'); END"); err != nil {
		t.Fatal(err)
	}
	if err := p.service.ReconcileRelayCleanup(p.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.CanvasEntryBindingByInbound(p.ctx, p.inbound.ID); err != nil {
		t.Fatalf("completed cleanup ran again: %v", err)
	}
}

func TestCanvasRemoveSharedTunnelPreserved(t *testing.T) {
	p, scheme, binding := cleanupFixture(t)
	shared, err := p.service.Repository.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "shared", Execution: "server", Layout: scheme.Layout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt}, "owner", p.users); err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.Repository.CanvasScheme(p.ctx, shared.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.Repository.Inbound(p.ctx, binding.InboundID, false); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	var objects int
	if err = p.database.QueryRow("SELECT count(*) FROM canvas_tunnel_objects").Scan(&objects); err != nil || objects == 0 {
		t.Fatalf("shared objects %d %v", objects, err)
	}
}

func TestCanvasRemoveAtomicDeletionAndPending(t *testing.T) {
	p, scheme, binding := cleanupFixture(t)
	if _, err := p.database.Exec("CREATE TRIGGER reject_cleanup BEFORE DELETE ON outbounds BEGIN SELECT RAISE(ABORT,'cleanup rejected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt}, "owner", p.users); err == nil || !strings.Contains(err.Error(), "cleanup rejected") {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.CanvasEntryBindingByInbound(p.ctx, binding.InboundID); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := p.database.QueryRow("SELECT count(*) FROM relay_cleanup_nodes").Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("pending %d %v", pending, err)
	}
}

func TestCanvasRemoveRejectsConsumerAddedBeforeGuardCapture(t *testing.T) {
	p, scheme, _ := cleanupFixture(t)
	plan, err := p.service.canvasRemovalScope(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	var target CanvasRemoveObject
	for _, object := range plan.Objects {
		if object.Kind == "outbound" && !object.Keep {
			target = object
			break
		}
	}
	if target.ID == "" {
		t.Fatal("fixture has no relay outbound")
	}
	if _, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "new consumer", NodeID: target.NodeID, Protocol: "socks5", Listen: "0.0.0.0", Port: 30001, Enabled: true, DefaultOutbound: target.ID}); err != nil {
		t.Fatal(err)
	}
	guards, err := p.service.canvasRemovalRows(p.ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.service.finishCanvasRemoval(p.ctx, plan, guards); err == nil {
		t.Fatal("deleted a provider newly referenced before guard capture")
	}
	if _, err := p.service.Repository.Outbound(p.ctx, target.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasRemoveAcknowledgesEntryChanges(t *testing.T) {
	for _, kind := range []string{"port", "groups", "unknown-baseline"} {
		t.Run(kind, func(t *testing.T) {
			p, scheme, binding := cleanupFixture(t)
			var err error
			switch kind {
			case "port":
				_, err = p.database.Exec("UPDATE inbounds SET port=20009 WHERE id=?", binding.InboundID)
			case "groups":
				entryPlanGroup(t, p, "changed-group", "mixed", binding.InboundID)
			case "unknown-baseline":
				_, err = p.database.Exec("UPDATE canvas_entry_bindings SET creation_config_json=NULL,creation_groups_json=NULL WHERE id=?", binding.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if result, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt}, "owner", p.users); err != nil || !result.Removed {
				t.Fatalf("remove %+v %v", result, err)
			}
		})
	}
}

func TestCanvasRemoveHTTPContract(t *testing.T) {
	for _, method := range []string{"POST", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			p, scheme, _ := cleanupFixture(t)
			cookie, csrf := p.session("Owner")
			path := "/api/v1/canvas-schemes/" + scheme.ID
			if method == "POST" {
				path += "/remove"
			}
			for _, tc := range []struct {
				stamp  string
				status int
			}{{"", 400}, {"stale", 409}, {scheme.UpdatedAt, 200}} {
				response := p.call(method, path, map[string]any{"updatedAt": tc.stamp, "restoreTargets": "ignored", "acknowledgeChanges": false, "extra": 42}, cookie, csrf)
				if response.Code != tc.status {
					t.Fatalf("%d %s", response.Code, response.Body.String())
				}
				if tc.status == 200 && (!strings.Contains(response.Body.String(), `"removed":true`) || !strings.Contains(response.Body.String(), `"pendingNodes":`)) {
					t.Fatal(response.Body.String())
				}
			}
			if response := p.call(method, path, map[string]any{"updatedAt": scheme.UpdatedAt}, cookie, csrf); response.Code != 404 {
				t.Fatalf("missing %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCanvasRetiredRemovalRoutesMissing(t *testing.T) {
	p := newCanvasPanel(t)
	cookie, csrf := p.session("Owner")
	for _, route := range []struct{ method, path string }{{"POST", "/api/v1/canvas-schemes/missing/remove-plan"}, {"GET", "/api/v1/canvas-schemes/missing/remove-status"}, {"GET", "/api/v1/relay-schemes/legacy-resources"}, {"POST", "/api/v1/relay-schemes/legacy-resources/reattach"}} {
		response := p.call(route.method, route.path, map[string]any{}, cookie, csrf)
		if response.Code != 404 {
			t.Fatalf("retired %s: %d %s", route.path, response.Code, response.Body.String())
		}
	}
}

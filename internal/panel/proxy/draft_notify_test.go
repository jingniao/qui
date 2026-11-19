package proxy

import (
	"context"
	"testing"
)

func TestInboundDraftNotificationsWithoutHTTP(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	calls := 0
	f.service.Repository.DraftChanged = func() { calls++ }
	input := InboundInput{Name: "quick entry", NodeID: f.nodeA, Protocol: "socks5", Listen: "127.0.0.1", Port: 12347, Enabled: true, DefaultOutbound: "direct"}
	i, err := f.service.SaveInbound(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("create notifications %d", calls)
	}
	input.Port++
	if _, err := f.service.SaveInbound(ctx, i.ID, input); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Repository.DeleteInbound(ctx, i.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("CRUD notifications %d", calls)
	}
	input.Port = -1
	if _, err := f.service.SaveInbound(ctx, "", input); err == nil {
		t.Fatal("invalid port accepted")
	}
	if calls != 3 {
		t.Fatal("failed save notified")
	}
}

func TestCanvasDraftNotificationIncludesCommittedEntryRoute(t *testing.T) {
	p := newPublishPanel(t)
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})
	scheme := savePlanScheme(t, p, layout)
	lastRoute := ""
	p.service.Repository.DraftChanged = func() {
		rows, _, err := p.service.Repository.Inbounds(p.ctx, p.nodeA, 1, 100, false)
		if err != nil {
			t.Error(err)
			return
		}
		for _, row := range rows {
			if row.Port == 20000 {
				lastRoute = row.DefaultOutbound
			}
		}
	}
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
	if lastRoute == "direct" || lastRoute != binding.AppliedDefaultOutbound {
		t.Fatalf("notification retained stale route %q vs %q", lastRoute, binding.AppliedDefaultOutbound)
	}
}

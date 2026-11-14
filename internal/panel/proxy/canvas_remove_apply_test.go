package proxy

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type removalDeployer struct {
	canvasDeployRecorder
	phases       [][]NodeSnapshot
	failAt       int
	rollbacks    int
	afterPublish func(int)
}

func (d *removalDeployer) PublishCanvasRemoval(_ context.Context, _, _ string, snapshots []NodeSnapshot) (CanvasRemovePublication, error) {
	d.phases = append(d.phases, snapshots)
	if d.afterPublish != nil {
		d.afterPublish(len(d.phases))
	}
	result := CanvasRemovePublication{CanvasPublishResult: CanvasPublishResult{Nodes: []CanvasPublishNode{}}}
	for _, snapshot := range snapshots {
		if err := snapshot.State.Validate(); err != nil {
			return result, err
		}
		result.Nodes = append(result.Nodes, CanvasPublishNode{NodeID: snapshot.State.NodeID, Status: "success"})
	}
	if len(d.phases) == d.failAt {
		return result, errors.New("remove agent failed")
	}
	return result, nil
}

func (d *removalDeployer) RollbackCanvasRemoval(context.Context, CanvasRemovePublication) error {
	d.rollbacks++
	return nil
}

func removeFixture(t *testing.T, mode string) (*publishPanel, CanvasScheme, CanvasEntryBindingRecord, *removalDeployer) {
	t.Helper()
	p := newPublishPanel(t)
	p.service.EntryGroups = p.service.Nodes.(CanvasEntryGroups)
	b := &CanvasEntryBinding{Mode: mode, Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
	if mode == "reuse" {
		b.InboundID = p.inbound.ID
		b.Protocol = ""
		b.PortMode = "auto"
		b.Port = nil
	}
	scheme := savePlanScheme(t, p, entryPlanLayout(p, b))
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
	d := &removalDeployer{}
	p.service.Deploy = d
	return p, scheme, binding, d
}

func TestCanvasRemoveCreateStopsEntryBeforeChannels(t *testing.T) {
	p, scheme, binding, deploy := removeFixture(t, "create")
	entryPlanGroup(t, p, "manual-group", "mixed", binding.InboundID)
	result, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	for _, publication := range result.Publications {
		for _, n := range publication.Nodes {
			if n.Name == "" {
				t.Error("removal publication missing node name")
			}
		}
	}
	if len(deploy.phases) != 2 {
		t.Fatalf("phases %d", len(deploy.phases))
	}
	if len(deploy.phases[0]) != 1 || deploy.phases[0][0].State.NodeID != p.nodeA {
		t.Fatalf("entry phase %+v", deploy.phases[0])
	}
	for _, in := range deploy.phases[0][0].State.Inbounds {
		if in.ID == binding.InboundID && in.Enabled {
			t.Fatal("entry still enabled")
		}
	}
	for _, table := range []string{"canvas_schemes", "canvas_entry_bindings", "canvas_tunnel_objects", "canvas_tunnels"} {
		var count int
		if err = p.database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count %d: %v", table, count, err)
		}
	}
	if _, err = p.service.Repository.Inbound(p.ctx, binding.InboundID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("entry survives: %v", err)
	}
	var members int
	if err = p.database.QueryRow("SELECT count(*) FROM node_group_inbounds WHERE inbound_id=?", binding.InboundID).Scan(&members); err != nil || members != 0 {
		t.Fatalf("members %d: %v", members, err)
	}
}

func TestCanvasRemoveReuseRestoresOriginal(t *testing.T) {
	p, scheme, binding, _ := removeFixture(t, "reuse")
	_, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt}, "owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	in, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, false)
	if err != nil || in.DefaultOutbound != "direct" {
		t.Fatalf("restored %+v: %v", in, err)
	}
}

func TestCanvasRemoveFailureRestoresDatabaseAndKeepsScheme(t *testing.T) {
	p, scheme, binding, deploy := removeFixture(t, "create")
	deploy.failAt = 2
	_, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users)
	if err == nil || !strings.Contains(err.Error(), "remove agent failed") {
		t.Fatalf("error %v", err)
	}
	in, readErr := p.service.Repository.Inbound(p.ctx, binding.InboundID, false)
	if readErr != nil || !in.Enabled || in.DefaultOutbound != binding.AppliedDefaultOutbound {
		t.Fatalf("restored %+v: %v", in, readErr)
	}
	if _, readErr = p.service.Repository.CanvasScheme(p.ctx, scheme.ID); readErr != nil {
		t.Fatal(readErr)
	}
	if deploy.rollbacks != 2 {
		t.Fatalf("rollback phases %d", deploy.rollbacks)
	}
	if _, err := p.service.Repository.canvasTunnel(p.ctx, scheme.Layout.Connections[0].TunnelID); err != nil {
		t.Fatalf("failure deleted tunnel: %v", err)
	}
}

func TestCanvasRemoveConcurrentEntryEditIsPreserved(t *testing.T) {
	p, scheme, binding, deploy := removeFixture(t, "create")
	deploy.afterPublish = func(phase int) {
		if phase == 1 {
			if _, err := p.database.Exec("UPDATE inbounds SET port=20001,default_outbound='block' WHERE id=?", binding.InboundID); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users)
	if err == nil {
		t.Fatal("concurrent change accepted")
	}
	in, readErr := p.service.Repository.Inbound(p.ctx, binding.InboundID, false)
	if readErr != nil || in.Port != 20001 || in.DefaultOutbound != "block" {
		t.Fatalf("edit lost %+v: %v", in, readErr)
	}
}

func TestCanvasRemoveCancellationKeepsTunnelRows(t *testing.T) {
	for _, phase := range []int{1, 2} {
		p, scheme, _, deploy := removeFixture(t, "create")
		ctx, cancel := context.WithCancel(p.ctx)
		deploy.afterPublish = func(n int) {
			if n == phase {
				cancel()
			}
		}
		result, err := p.service.RemoveCanvasScheme(ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users)
		cancel()
		if !errors.Is(err, context.Canceled) || result.Removed {
			t.Fatalf("phase %d: %+v %v", phase, result, err)
		}
		if _, err := p.service.Repository.canvasTunnel(p.ctx, scheme.Layout.Connections[0].TunnelID); err != nil {
			t.Fatal(err)
		}
		if _, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCanvasRemoveTunnelDeleteFailureRollsBackFinalTransaction(t *testing.T) {
	p, scheme, binding, deploy := removeFixture(t, "create")
	if _, err := p.database.Exec(`CREATE TRIGGER reject_tunnel_delete BEFORE DELETE ON canvas_tunnels BEGIN SELECT RAISE(ABORT,'controlled tunnel delete failure'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := p.service.RemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users)
	if err == nil || !strings.Contains(err.Error(), "controlled tunnel delete failure") || result.Removed {
		t.Fatalf("result %+v %v", result, err)
	}
	if _, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.canvasTunnel(p.ctx, scheme.Layout.Connections[0].TunnelID); err != nil {
		t.Fatal(err)
	}
	in, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, false)
	if err != nil || !in.Enabled || deploy.rollbacks != 2 {
		t.Fatalf("rollback %+v %v phases=%d", in, err, deploy.rollbacks)
	}
}

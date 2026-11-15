package proxy

import (
	"encoding/json"
	"errors"
	"proxycontrol/internal/panel/node"
	"testing"
)

func TestNodeDeletionManualInboundReferenceReturnsBlocker(t *testing.T) {
	p := newCanvasPanel(t)
	out, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "manual relay", NodeID: p.nodeA, Protocol: "socks5", Enabled: true, TargetInboundID: p.inbound.ID, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	nodes := p.service.Nodes.(*node.Repository)
	nodes.RelayDeletion = p.service
	_, err = nodes.DeleteWithResult(p.ctx, p.inbound.NodeID, nil, nil, "owner", "ip")
	var blocked *node.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("want structured node blocker; got %v", err)
	}
	found := false
	for _, b := range blocked.Items {
		if b.ID == out.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing outbound: %+v", blocked)
	}
	if _, err = p.service.Repository.Inbound(p.ctx, p.inbound.ID, false); err != nil {
		t.Fatal(err)
	}
}

func TestRelayDeletionRejectsUnfinalizedTunnelPublication(t *testing.T) {
	for _, status := range []string{"draft", "success", "failed"} {
		t.Run(status, func(t *testing.T) {
			p, exitID, scheme, _, _, _ := deletionCascadeFixture(t)
			raw, _ := json.Marshal(map[string]any{"nodes": []string{p.nodeA, p.nodeB}})
			if _, err := p.database.Exec("INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at,tunnel_publication_json,tunnel_finalized) VALUES('unfinalized','','publishing','owner',?,'now',?,0)", status, string(raw)); err != nil {
				t.Fatal(err)
			}
			preview, err := p.service.PreviewInboundDeletion(p.ctx, exitID)
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.Blockers) != 1 || preview.Blockers[0].Kind != "deployment_batch" {
				t.Fatalf("preview %+v", preview)
			}
			_, err = p.service.DeleteInbound(p.ctx, exitID)
			var conflict *tunnelError
			if !errors.As(err, &conflict) || conflict.Code != "inbound_delete_blocked" {
				t.Fatalf("delete: %v", err)
			}
			nodes := p.service.Nodes.(*node.Repository)
			nodes.RelayDeletion = p.service
			_, err = nodes.DeleteWithResult(p.ctx, p.nodeB, nil, nil, "owner", "ip")
			var blocked *node.BlockedError
			if !errors.As(err, &blocked) {
				t.Fatalf("node delete: %v", err)
			}
			if _, err = p.service.Repository.CanvasScheme(p.ctx, scheme.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRelayDeletionRejectsReferenceAddedAfterPreparation(t *testing.T) {
	p := newCanvasPanel(t)
	plan, err := p.service.prepareRelayDeletion(p.ctx, p.inbound.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "late relay", NodeID: p.nodeA, Protocol: "socks5", Enabled: true, TargetInboundID: p.inbound.ID, SupportTCP: true, SupportUDP: true}); err != nil {
		t.Fatal(err)
	}
	tx, err := p.database.BeginTx(p.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = p.service.applyRelayDeletion(p.ctx, tx, plan); err == nil {
		t.Fatal("accepted changed ownership graph")
	}
}

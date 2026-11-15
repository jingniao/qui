package deployment

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/proxy"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// Pause at the real preparation/dispatch boundary, without replacing either
// the preparation transaction or the deployment implementation.
type preparationObserver struct {
	*CanvasPublisher
	before func(string, []proxy.NodeSnapshot)
}

func (p *preparationObserver) PublishTunnels(ctx context.Context, batchID, name, author string, snapshots []proxy.NodeSnapshot) (proxy.CanvasPublishResult, error) {
	p.before(batchID, snapshots)
	return p.CanvasPublisher.PublishTunnels(ctx, batchID, name, author, snapshots)
}

func directPreparationRule(t *testing.T, h *gostHosts, port int) proxy.TunnelRule {
	t.Helper()
	rule, err := h.service.SaveTunnelRule(h.ctx, "", proxy.TunnelRuleInput{Name: "direct", EntryNodeID: h.alpine, PortMode: "manual", ListenPort: portValue(port), Network: "both", Target: proxy.TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func TestTunnelPreparationDifferentRulesReserveSharedNode(t *testing.T) {
	h := newGostHosts(t)
	a := directPreparationRule(t, h, 30100)
	b := directPreparationRule(t, h, 30101)
	publisher := h.service.Deploy.(*CanvasPublisher)
	publisher.tunnelWaitTimeout = time.Millisecond
	other := &proxy.Service{Repository: h.service.Repository, Nodes: h.service.Nodes, Deploy: &preparationObserver{CanvasPublisher: publisher, before: func(_ string, snapshots []proxy.NodeSnapshot) {
		t.Errorf("B materialized and captured %d node snapshots while A was preparing", len(snapshots))
	}}}
	h.service.Deploy = &preparationObserver{CanvasPublisher: publisher, before: func(batchID string, _ []proxy.NodeSnapshot) {
		// A has committed its listeners but has not created items or started.
		// B owns a different rule and must fail before taking a node snapshot.
		_, err := other.PublishTunnels(h.ctx, proxy.PublishSelection{RuleIDs: []string{b.ID}}, "owner", h.users)
		if !errors.Is(err, ErrNodeBusy) {
			t.Errorf("second preparation on shared node must be busy, got %v", err)
		}
		var batches, listeners int
		if err = h.database.QueryRow("SELECT count(*) FROM deployment_batches").Scan(&batches); err != nil {
			t.Fatal(err)
		}
		if err = h.database.QueryRow("SELECT count(*) FROM inbounds WHERE node_id=? AND port=30101", h.alpine).Scan(&listeners); err != nil {
			t.Fatal(err)
		}
		if batches != 1 || listeners != 0 {
			t.Errorf("rejected preparation mutated database: batches=%d B listeners=%d", batches, listeners)
		}
	}}
	result, err := h.service.PublishTunnels(h.ctx, proxy.PublishSelection{RuleIDs: []string{a.ID}}, "owner", h.users)
	if err != nil || result.Status != "publishing" {
		t.Fatalf("A must still start: %+v %v", result, err)
	}
	batch, err := h.repository.Batch(h.ctx, result.BatchID)
	if err != nil || len(batch.Items) != 1 {
		t.Fatalf("single-node batch: %+v %v", batch, err)
	}
	state, err := h.repository.State(h.ctx, batch.Items[0].Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range state.Inbounds {
		if in.Port == 30101 {
			t.Fatal("A snapshot captured rejected B listener")
		}
	}
	if err = h.repository.Result(h.ctx, h.alpine, &pb.ApplyResult{Revision: batch.Items[0].Revision, Success: true}); err != nil {
		t.Fatal(err)
	}
	reconcileTunnelPublications(t, h, false)
	h.service.Deploy = publisher
	publisher.tunnelWaitTimeout = 0
	result, _ = driveTunnelPublish(t, h, proxy.PublishSelection{RuleIDs: []string{b.ID}}, "")
	if result.Status != "success" {
		t.Fatalf("B cannot publish after A finalization: %+v", result)
	}
}

func TestTunnelPreparationRejectsStartAndHTTP(t *testing.T) {
	for _, entry := range []string{"repository", "http"} {
		t.Run(entry, func(t *testing.T) {
			h := newGostHosts(t)
			rule := directPreparationRule(t, h, 30100)
			publisher := h.service.Deploy.(*CanvasPublisher)
			publisher.tunnelWaitTimeout = time.Millisecond
			h.service.Deploy = &preparationObserver{CanvasPublisher: publisher, before: func(batchID string, _ []proxy.NodeSnapshot) {
				func() {
					defer func() {
						if p := recover(); p != nil {
							t.Errorf("preparing batch panicked through %s: %v", entry, p)
						}
					}()
					if entry == "repository" {
						if err := h.repository.StartBatch(h.ctx, batchID); err == nil || !strings.Contains(err.Error(), "prepar") {
							t.Errorf("expected preparation conflict, got %v", err)
						}
					} else {
						handler := &Handler{Repository: h.repository}
						req := httptest.NewRequest("POST", "/api/v1/deployment-batches/"+batchID+"/deploy", nil)
						req.SetPathValue("id", batchID)
						response := httptest.NewRecorder()
						handler.deployBatch(response, req)
						if response.Code != 409 || !strings.Contains(response.Body.String(), "deployment_conflict") {
							t.Errorf("expected HTTP conflict: %d %s", response.Code, response.Body.String())
						}
					}
				}()
				batch, err := h.repository.Batch(h.ctx, batchID)
				if err != nil || batch.Status != "draft" || len(batch.Items) != 0 {
					t.Fatalf("start changed preparation: %+v %v", batch, err)
				}
				var queued int
				if err = h.database.QueryRow("SELECT count(*) FROM deployment_targets").Scan(&queued); err != nil || queued != 0 {
					t.Fatalf("preparing batch queued work: %d %v", queued, err)
				}
			}}
			result, err := h.service.PublishTunnels(h.ctx, proxy.PublishSelection{RuleIDs: []string{rule.ID}}, "owner", h.users)
			if err != nil || result.Status != "publishing" {
				t.Fatalf("prepared single-node batch failed to start: %+v %v", result, err)
			}
			if err = h.repository.StartBatch(h.ctx, result.BatchID); err != nil {
				t.Fatalf("repeated start: %v", err)
			}
		})
	}
}

func TestTunnelPreparationChecksUnchangedNodes(t *testing.T) {
	for _, changeEntry := range []bool{false, true} {
		name := "no_changes"
		if changeEntry {
			name = "only_entry_changes"
		}
		t.Run(name, func(t *testing.T) {
			h, _, rule := managedTunnelFixture(t, false)
			selection := proxy.PublishSelection{RuleIDs: []string{rule.ID}}
			driveTunnelPublish(t, h, selection, "")
			if changeEntry {
				input := rule.TunnelRuleInput
				input.ListenPort = portValue(30002)
				if _, err := h.service.SaveTunnelRule(h.ctx, rule.ID, input); err != nil {
					t.Fatal(err)
				}
			}
			// The exit is unchanged in both cases and would be pruned from items.
			revision, err := h.repository.Current(h.ctx, h.us2)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = h.repository.RollbackNode(h.ctx, h.us2, revision); err != nil {
				t.Fatal(err)
			}
			var before, after int
			if err = h.database.QueryRow("SELECT count(*) FROM deployment_batches").Scan(&before); err != nil {
				t.Fatal(err)
			}
			h.service.Deploy.(*CanvasPublisher).tunnelWaitTimeout = time.Millisecond
			if _, err = h.service.PublishTunnels(h.ctx, selection, "owner", h.users); !errors.Is(err, ErrNodeBusy) {
				t.Errorf("unchanged exit rollback must block preparation: %v", err)
			}
			if err = h.database.QueryRow("SELECT count(*) FROM deployment_batches").Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != before || h.inbounds(h.alpine)[0].Port != 30001 {
				t.Errorf("busy plan was materialized: batches before=%d after=%d", before, after)
			}
		})
	}
}

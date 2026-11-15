package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/testutil/dbtest"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestTunnelPublicationCancelledRequestStillFinishes(t *testing.T) {
	h, tunnel, rule := managedTunnelFixture(t, false)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	type response struct {
		result proxy.PublishResult
		err    error
	}
	done := make(chan response, 1)
	go func() {
		result, err := h.service.PublishTunnels(ctx, proxy.PublishSelection{RuleIDs: []string{rule.ID}}, "owner", h.users)
		done <- response{result, err}
	}()
	deadline := time.After(5 * time.Second)
	cancelled := false
	for {
		select {
		case res := <-done:
			if res.err != nil {
				t.Fatalf("request cancellation aborted durable publication: %v", res.err)
			}
			stored, err := h.service.Repository.Tunnel(h.ctx, tunnel.ID)
			if err != nil || stored.Status != "published" || stored.PublishedVersion != stored.DraftVersion {
				t.Fatalf("publication not finalized: status=%s draft=%d published=%d err=%v", stored.Status, stored.DraftVersion, stored.PublishedVersion, err)
			}
			var snapshot *string
			if err = h.database.QueryRow("SELECT published_snapshot FROM tunnel_rules WHERE id=?", rule.ID).Scan(&snapshot); err != nil || snapshot == nil {
				t.Fatalf("subscription snapshot missing: %v", err)
			}
			return
		case <-deadline:
			t.Fatal("publication did not finish")
		case <-time.After(5 * time.Millisecond):
			for _, nodeID := range []string{h.us2, h.alpine} {
				pending, err := h.repository.Pending(h.ctx, nodeID)
				if err != nil {
					t.Fatal(err)
				}
				if pending == nil {
					continue
				}
				if !cancelled {
					cancel()
					cancelled = true
					time.Sleep(15 * time.Millisecond)
				}
				var state desiredstate.State
				if err = json.Unmarshal(pending.DesiredStateJson, &state); err != nil {
					t.Fatal(err)
				}
				if err = h.repository.Result(h.ctx, nodeID, &pb.ApplyResult{Revision: state.Revision, Success: true}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func interruptedRulePublication(t *testing.T) (*gostHosts, proxy.TunnelRule, proxy.PublishResult) {
	t.Helper()
	h, _, rule := managedTunnelFixture(t, false)
	driveTunnelPublish(t, h, proxy.PublishSelection{RuleIDs: []string{rule.ID}}, "")
	stored, err := h.service.Repository.TunnelRule(h.ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := stored.TunnelRuleInput
	input.ListenPort = portValue(30002)
	if _, err = h.service.SaveTunnelRule(h.ctx, rule.ID, input); err != nil {
		t.Fatal(err)
	}
	h.service.Deploy.(*CanvasPublisher).tunnelWaitTimeout = 25 * time.Millisecond
	result, err := h.service.PublishTunnels(h.ctx, proxy.PublishSelection{RuleIDs: []string{rule.ID}}, "owner", h.users)
	if err != nil {
		t.Fatal(err)
	}
	if result.BatchID == "" || len(result.Servers) == 0 || result.Servers[len(result.Servers)-1].Status != "publishing" {
		t.Fatalf("timeout must retain a durable publishing batch: %+v", result)
	}
	if got := h.inbounds(h.alpine)[0].Port; got != 30002 {
		t.Fatalf("timeout restored an in-flight listener: %d", got)
	}
	return h, stored, result
}

func reconcileTunnelPublications(t *testing.T, h *gostHosts, startup bool) {
	t.Helper()
	finalizer, ok := any(h.service).(interface {
		ReconcileTunnelPublications(context.Context, bool) error
	})
	if !ok {
		t.Fatal("durable tunnel publication reconciliation unavailable")
	}
	if err := finalizer.ReconcileTunnelPublications(h.ctx, startup); err != nil {
		t.Fatal(err)
	}
}

func assertRulePublication(t *testing.T, h *gostHosts, rule proxy.TunnelRule, success bool) {
	t.Helper()
	stored, err := h.service.Repository.TunnelRule(h.ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantPort := 30001
	if success {
		wantPort = 30002
	}
	if got := h.inbounds(h.alpine)[0].Port; got != wantPort {
		t.Fatalf("listener=%d want %d", got, wantPort)
	}
	var raw string
	if err = h.database.QueryRow("SELECT published_snapshot FROM tunnel_rules WHERE id=?", rule.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var snapshot struct{ Port int }
	if err = json.Unmarshal([]byte(raw), &snapshot); err != nil || snapshot.Port != wantPort {
		t.Fatalf("snapshot port=%d want=%d err=%v", snapshot.Port, wantPort, err)
	}
	if success && stored.DraftVersion != stored.PublishedVersion || !success && stored.DraftVersion == stored.PublishedVersion {
		t.Fatalf("versions draft=%d published=%d success=%v", stored.DraftVersion, stored.PublishedVersion, success)
	}
	old, err := h.service.Repository.CheckPort(h.ctx, h.alpine, 30001, nil)
	if err != nil || old.Free != success {
		t.Fatalf("old claim free=%v success=%v err=%v", old.Free, success, err)
	}
	current, err := h.service.Repository.CheckPort(h.ctx, h.alpine, 30002, nil)
	if err != nil || current.Free {
		t.Fatalf("new draft claim lost: %v", err)
	}
}

func TestTunnelPublicationTimeoutThenFailure(t *testing.T) {
	h, rule, result := interruptedRulePublication(t)
	batch, err := h.repository.Batch(h.ctx, result.BatchID)
	if err != nil || len(batch.Items) != 1 {
		t.Fatalf("single changed node must be durable: %v %v", batch, err)
	}
	item := batch.Items[0]
	if err = h.repository.Result(h.ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, RolledBack: true, Code: "apply_failed"}); err != nil {
		t.Fatal(err)
	}
	reconcileTunnelPublications(t, h, false)
	assertRulePublication(t, h, rule, false)
	reconcileTunnelPublications(t, h, false)
	assertRulePublication(t, h, rule, false)
}

func TestTunnelPublicationRestartBeforeFinalization(t *testing.T) {
	h, rule, result := interruptedRulePublication(t)
	batch, err := h.repository.Batch(h.ctx, result.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	item := batch.Items[0]
	if err = h.repository.Result(h.ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, Success: true}); err != nil {
		t.Fatal(err)
	}
	var sequence int
	var name, filename string
	if err = h.database.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &filename); err != nil {
		t.Fatal(err)
	}
	key := h.repository.Vault
	if err = h.database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := dbtest.Open(h.ctx, filepath.Clean(filename))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	h.database = database
	h.repository = &Repository{Database: database, Vault: key}
	h.users = &user.Repository{Database: database, Vault: key}
	h.service = &proxy.Service{Repository: &proxy.Repository{Database: database, Vault: key}, Nodes: &node.Repository{Database: database}, Deploy: &CanvasPublisher{Repository: h.repository}}
	reconcileTunnelPublications(t, h, true)
	assertRulePublication(t, h, rule, true)
	reconcileTunnelPublications(t, h, true)
	assertRulePublication(t, h, rule, true)
}

func TestTunnelPublicationPendingReservationsAndAtomicRetry(t *testing.T) {
	h, rule, result := interruptedRulePublication(t)
	if _, err := h.service.SaveTunnelRule(h.ctx, rule.ID, rule.TunnelRuleInput); err == nil {
		t.Fatal("pending publication allowed an edit")
	}
	if _, err := h.service.PublishTunnels(h.ctx, proxy.PublishSelection{RuleIDs: []string{rule.ID}}, "owner", h.users); err == nil {
		t.Fatal("pending publication allowed a second publish")
	}
	batch, err := h.repository.Batch(h.ctx, result.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	item := batch.Items[0]
	if err = h.repository.Result(h.ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, Success: true}); err != nil {
		t.Fatal(err)
	}
	tx, err := h.database.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = nodeAvailable(h.ctx, tx, item.NodeID, "")
	tx.Rollback()
	if !errors.Is(err, ErrNodeBusy) {
		t.Fatalf("unfinalized success lost node reservation: %v", err)
	}
	if _, err = h.database.Exec(`CREATE TRIGGER reject_tunnel_finalize BEFORE UPDATE OF tunnel_finalized ON deployment_batches WHEN NEW.tunnel_finalized=1 BEGIN SELECT RAISE(ABORT,'finalize interrupted'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = h.service.FinalizeTunnelPublication(h.ctx, result.BatchID); err == nil || !strings.Contains(err.Error(), "finalize interrupted") {
		t.Fatalf("transaction error not exposed: %v", err)
	}
	var finalized bool
	if err = h.database.QueryRow("SELECT tunnel_finalized FROM deployment_batches WHERE id=?", result.BatchID).Scan(&finalized); err != nil || finalized {
		t.Fatalf("failed transaction marked complete: %v", err)
	}
	stored, err := h.service.Repository.TunnelRule(h.ctx, rule.ID)
	if err != nil || stored.PublishedVersion != rule.PublishedVersion {
		t.Fatalf("failed transaction advanced version: %+v %v", stored, err)
	}
	var raw string
	if err = h.database.QueryRow("SELECT published_snapshot FROM tunnel_rules WHERE id=?", rule.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var snapshot struct{ Port int }
	if err = json.Unmarshal([]byte(raw), &snapshot); err != nil || snapshot.Port != 30001 {
		t.Fatalf("failed transaction advanced snapshot: %s %v", raw, err)
	}
	old, err := h.service.Repository.CheckPort(h.ctx, h.alpine, 30001, nil)
	if err != nil || old.Free {
		t.Fatalf("failed transaction released claim: %v", err)
	}
	if _, err = h.database.Exec("DROP TRIGGER reject_tunnel_finalize"); err != nil {
		t.Fatal(err)
	}
	reconcileTunnelPublications(t, h, true)
	assertRulePublication(t, h, rule, true)
}

func TestTunnelPublicationInventoryBefore0149(t *testing.T) {
	h, _, _ := managedTunnelFixture(t, false)
	for _, query := range []string{"ALTER TABLE deployment_batches DROP COLUMN tunnel_publication_json", "ALTER TABLE deployment_batches DROP COLUMN tunnel_finalized", "DELETE FROM schema_migrations WHERE version='0149_tunnel_publication_finalization.sql'"} {
		if _, err := h.database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := backup.ReadInventory(h.ctx, h.database); err != nil {
		t.Fatalf("pre0149 inventory failed: %v", err)
	}
}

func TestTunnelPublicationCorruptBatchDoesNotBlockNext(t *testing.T) {
	for _, startup := range []bool{false, true} {
		t.Run(map[bool]string{false: "periodic", true: "startup"}[startup], func(t *testing.T) {
			h, rule, result := interruptedRulePublication(t)
			batch, err := h.repository.Batch(h.ctx, result.BatchID)
			if err != nil {
				t.Fatal(err)
			}
			item := batch.Items[0]
			if err = h.repository.Result(h.ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, Success: true}); err != nil {
				t.Fatal(err)
			}
			const badID = "corrupt-publication"
			if _, err = h.database.Exec("INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at,tunnel_publication_json,tunnel_finalized) VALUES(?,'','corrupt','owner',?,'2000-01-01T00:00:00Z',?,0)", badID, map[bool]string{false: "failed", true: "draft"}[startup], `{"phase":"preparing","nodes":[],"owners":[],"sealed":"Y29ycnVwdA=="}`); err != nil {
				t.Fatal(err)
			}
			err = h.service.ReconcileTunnelPublications(h.ctx, startup)
			if err == nil || !strings.Contains(err.Error(), badID) {
				t.Fatalf("missing batch failure: %v", err)
			}
			var finalized bool
			if err = h.database.QueryRow("SELECT tunnel_finalized FROM deployment_batches WHERE id=?", result.BatchID).Scan(&finalized); err != nil || !finalized {
				t.Fatalf("second batch not finalized: %v", err)
			}
			if err = h.database.QueryRow("SELECT tunnel_finalized FROM deployment_batches WHERE id=?", badID).Scan(&finalized); err != nil || finalized {
				t.Fatalf("corrupt batch lost reservation: %v", err)
			}
			assertRulePublication(t, h, rule, true)
		})
	}
}

package deployment

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"sync"
	"testing"
	"time"
)

func batchFixture(t *testing.T) (*Repository, []desiredstate.State, func()) {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(directory, "panel.db")
	database, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	configurationVault, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	r := &Repository{Database: database, Vault: configurationVault}
	t.Cleanup(func() { r.Database.Close() })
	states := []desiredstate.State{}
	nodes := &node.Repository{Database: database}
	for index, name := range []string{"exit", "middle", "entry"} {
		n, _, _, err := nodes.CreateEnrollment(ctx, name, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Enabled: true, Listen: "127.0.0.1", Port: 2100 + index, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}})
	}
	reopen := func() {
		t.Helper()
		if err := r.Database.Close(); err != nil {
			t.Fatal(err)
		}
		var err error
		r.Database, err = db.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
	}
	return r, states, reopen
}

func applySnapshot(t *testing.T, r *Repository, state desiredstate.State) int64 {
	t.Helper()
	ctx := context.Background()
	revision, err := r.Create(ctx, state, "owner", "previous configuration")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Result(ctx, state.NodeID, &pb.ApplyResult{Revision: revision.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	return revision.ID
}

func TestBatchPublicationReverseRollbackPersistenceAndReservations(t *testing.T) {
	ctx := context.Background()
	r, states, reopen := batchFixture(t)
	previous := make([]int64, len(states))
	for index, state := range states {
		previous[index] = applySnapshot(t, r, state)
	}
	batch, err := r.CreateBatch(ctx, "chain", "chain publication", "owner", states)
	if err != nil {
		t.Fatal(err)
	}
	// Rollback targets must be captured at start, not when the draft was made.
	previous[1] = applySnapshot(t, r, states[1])
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- r.StartBatch(ctx, batch.ID) }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal("duplicate concurrent start", err)
		}
	}
	batch, err = r.Batch(ctx, batch.ID)
	if err != nil || batch.Items[1].PreviousRevision != previous[1] {
		t.Fatal("stale draft rollback target", err)
	}
	other, err := r.CreateBatch(ctx, "other-chain", "overlapping batch", "owner", states[1:])
	if err != nil {
		t.Fatal(err)
	}
	if err := r.StartBatch(ctx, other.ID); err == nil {
		t.Fatal("overlapping batch used a waiting node")
	}
	standalone, err := r.Create(ctx, states[2], "owner", "competing deployment")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Deploy(ctx, standalone.ID); err == nil {
		t.Fatal("standalone deployment used a waiting node")
	}
	if _, err := r.RollbackNode(ctx, states[2].NodeID, previous[2]); err == nil {
		t.Fatal("standalone rollback used a reserved node")
	}
	if err := r.Deploy(ctx, other.Items[0].Revision); err == nil {
		t.Fatal("batch item published without its batch")
	}
	for index, item := range batch.Items {
		for checkIndex, check := range batch.Items {
			pending, err := r.Pending(ctx, check.NodeID)
			if err != nil || (pending != nil) != (checkIndex == index) {
				t.Fatal("publication order violated", index, checkIndex, err)
			}
		}
		reopen()
		result := &pb.ApplyResult{Revision: item.Revision, Success: true}
		if err := r.Result(ctx, item.NodeID, result); err != nil {
			t.Fatal(err)
		}
		if err := r.Result(ctx, item.NodeID, result); err != nil {
			t.Fatal("duplicate receipt changed batch", err)
		}
	}
	if err := r.RollbackBatch(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.RollbackBatch(ctx, batch.ID); err != nil {
		t.Fatal("duplicate rollback", err)
	}
	for index := len(batch.Items) - 1; index >= 0; index-- {
		item := batch.Items[index]
		for checkIndex, check := range batch.Items {
			pending, err := r.PendingRollback(ctx, check.NodeID)
			if err != nil || (pending != nil) != (checkIndex == index) {
				t.Fatal("rollback order violated", index, checkIndex, err)
			}
		}
		command, _ := r.PendingRollback(ctx, item.NodeID)
		if command.Revision != previous[index] {
			t.Fatal("wrong rollback target")
		}
		reopen()
		result := &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Success: true}
		if err := r.Result(ctx, "another-node", result); err == nil {
			t.Fatal("another node acknowledged rollback")
		}
		if err := r.Result(ctx, item.NodeID, result); err != nil {
			t.Fatal(err)
		}
		if err := r.Result(ctx, item.NodeID, result); err != nil {
			t.Fatal("duplicate rollback receipt", err)
		}
		published, err := r.Published(ctx, item.NodeID)
		if err != nil || published.Revision != previous[index] {
			t.Fatal("subscription source does not reflect restored version", err)
		}
	}
	batch, _ = r.Batch(ctx, batch.ID)
	if batch.Status != "rolled_back" {
		t.Fatal(batch.Status)
	}
	if err := r.Deploy(ctx, standalone.ID); err != nil {
		t.Fatal("completed rollback did not release reservation", err)
	}
}

func TestBatchFailureAtEveryHopRestoresFirstDeploymentToZero(t *testing.T) {
	for failingIndex := 0; failingIndex < 3; failingIndex++ {
		t.Run([]string{"exit", "middle", "entry"}[failingIndex], func(t *testing.T) {
			ctx := context.Background()
			r, states, reopen := batchFixture(t)
			batch, err := r.CreateBatch(ctx, "chain", "first publication", "owner", states)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.StartBatch(ctx, batch.ID); err != nil {
				t.Fatal(err)
			}
			for index := 0; index <= failingIndex; index++ {
				item := batch.Items[index]
				if err := r.Result(ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, Success: index < failingIndex, Code: "test_result"}); err != nil {
					t.Fatal(err)
				}
			}
			reopen()
			for index := failingIndex - 1; index >= 0; index-- {
				item := batch.Items[index]
				command, err := r.PendingRollback(ctx, item.NodeID)
				if err != nil || command == nil || command.Revision != 0 {
					t.Fatal("first publication did not restore empty state", err)
				}
				if err := r.Result(ctx, item.NodeID, &pb.ApplyResult{CommandId: command.CommandId, Revision: 0, Success: true}); err != nil {
					t.Fatal(err)
				}
				if current, err := r.Current(ctx, item.NodeID); err != nil || current != 0 {
					t.Fatal("reset node retained a runtime revision")
				}
			}
			batch, _ = r.Batch(ctx, batch.ID)
			if batch.Status != "rolled_back" {
				t.Fatal("failure did not finish rollback", batch.Status)
			}
			for _, item := range batch.Items {
				if pending, err := r.Pending(ctx, item.NodeID); err != nil || pending != nil {
					t.Fatal("later hop was dispatched after failure")
				}
			}
		})
	}
}

func TestBatchRefusesStaleManualRollbackAndRetriesFailedRollback(t *testing.T) {
	ctx := context.Background()
	r, states, _ := batchFixture(t)
	batch, _ := r.CreateBatch(ctx, "chain", "publication", "owner", states)
	if err := r.StartBatch(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	for _, item := range batch.Items {
		if err := r.Result(ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.RollbackBatch(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	entry := batch.Items[2]
	command, _ := r.PendingRollback(ctx, entry.NodeID)
	if err := r.Result(ctx, entry.NodeID, &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Code: "rollback_failed"}); err != nil {
		t.Fatal(err)
	}
	competing, _ := r.Create(ctx, states[0], "owner", "competing")
	if err := r.Deploy(ctx, competing.ID); err == nil {
		t.Fatal("failed rollback released chain reservation")
	}
	if err := r.RollbackBatch(ctx, batch.ID); err != nil {
		t.Fatal("could not retry failed rollback", err)
	}
	retry, _ := r.PendingRollback(ctx, entry.NodeID)
	if retry == nil || retry.CommandId == command.CommandId {
		t.Fatal("retry reused failed command receipt")
	}
	for index := 2; index >= 0; index-- {
		item := batch.Items[index]
		command, _ := r.PendingRollback(ctx, item.NodeID)
		if err := r.Result(ctx, item.NodeID, &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	second, _ := r.CreateBatch(ctx, "chain", "another publication", "owner", states)
	if err := r.StartBatch(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	for _, item := range second.Items {
		if err := r.Result(ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	applySnapshot(t, r, states[1])
	if err := r.RollbackBatch(ctx, second.ID); err == nil {
		t.Fatal("stale batch rollback overwrote a newer configuration")
	}
}

func TestBatchNextHopDependencyFailureCommitsReceiptAndRollsBack(t *testing.T) {
	ctx := context.Background()
	r, states, _ := batchFixture(t)
	// The captured batch must be internally consistent: CreateBatch validates
	// all static relay dependencies before creating versions
	// (docs/relay-deployment-batches.md). The target is intentionally left
	// unpublished so the dependency is only found at dispatch, exercising the
	// rollback of an already applied exit.
	credential := desiredstate.Credential{ID: "relay", Protocol: "socks5", Username: "relay", Secret: "relay-test-secret", Internal: true}
	states[0].Credentials = []desiredstate.Credential{credential}
	states[0].Inbounds[0].CredentialIDs = []string{credential.ID}
	states[1].Credentials = []desiredstate.Credential{credential}
	states[1].Outbounds = append(states[1].Outbounds, desiredstate.Outbound{ID: "next", Protocol: "socks5", Address: "127.0.0.1", Port: states[0].Inbounds[0].Port, TargetNodeID: states[0].NodeID, CredentialID: credential.ID, Enabled: true})
	states[1].Inbounds[0].DefaultOutbound = "next"
	batch, err := r.CreateBatch(ctx, "chain", "unpublished relay target", "owner", states)
	if err != nil {
		t.Fatal(err)
	}
	// Disable the relay source after creation so the dependency is only
	// rejected at dispatch, exercising the rollback of the applied exit.
	if _, err := r.Database.Exec("UPDATE nodes SET enabled=0 WHERE id=?", states[1].NodeID); err != nil {
		t.Fatal(err)
	}
	if err := r.StartBatch(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	first := batch.Items[0]
	if err := r.Result(ctx, first.NodeID, &pb.ApplyResult{Revision: first.Revision, Success: true}); err != nil {
		t.Fatal("dependency failure undid successful receipt", err)
	}
	batch, _ = r.Batch(ctx, batch.ID)
	if batch.Status != "rolling_back" || batch.Items[1].Status != "failed" {
		t.Fatal("invalid next hop was not rejected", batch)
	}
	if pending, _ := r.Pending(ctx, states[1].NodeID); pending != nil {
		t.Fatal("invalid relay source was dispatched")
	}
	if rollback, _ := r.PendingRollback(ctx, first.NodeID); rollback == nil {
		t.Fatal("successfully applied exit was not rolled back")
	}
}

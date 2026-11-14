package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/proxy"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestCanvasRemovalWaitsAndRollsBackSingleRevision(t *testing.T) {
	r, states, _ := batchFixture(t)
	previous := applySnapshot(t, r, states[0])
	p := &CanvasPublisher{Repository: r}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state := states[0]
	state.Inbounds = nil
	type response struct {
		publication proxy.CanvasRemovePublication
		err         error
	}
	finished := make(chan response, 1)
	go func() {
		result, err := p.PublishCanvasRemoval(ctx, "remove", "owner", batchSnapshots([]desiredstate.State{state}))
		finished <- response{result, err}
	}()
	var result proxy.CanvasRemovePublication
	for {
		select {
		case answer := <-finished:
			if answer.err != nil {
				t.Fatal(answer.err)
			}
			result = answer.publication
			goto published
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond * 10):
			pending, err := r.Pending(ctx, state.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			if pending != nil {
				var applied desiredstate.State
				if err = json.Unmarshal(pending.DesiredStateJson, &applied); err != nil {
					t.Fatal(err)
				}
				if err = r.Result(ctx, state.NodeID, &pb.ApplyResult{Revision: applied.Revision, Success: true}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
published:
	if result.Previous[state.NodeID] != previous || len(result.Nodes) != 1 || result.Nodes[0].Status != "success" {
		t.Fatalf("result %+v", result)
	}
	rolled := make(chan error, 1)
	go func() { rolled <- p.RollbackCanvasRemoval(ctx, result) }()
	for {
		select {
		case err := <-rolled:
			if err != nil {
				t.Fatal(err)
			}
			return
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond * 10):
			command, err := r.PendingRollback(ctx, state.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			if command != nil {
				if command.Revision != previous {
					t.Fatalf("rollback %d", command.Revision)
				}
				if err = r.CommandResult(ctx, state.NodeID, &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Success: true}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestCanvasRemovalPublisherKeepsRevisionOnStatusReadFailure(t *testing.T) {
	r, states, _ := batchFixture(t)
	if _, err := r.Database.Exec("CREATE TRIGGER removal_missing_target AFTER INSERT ON deployment_targets BEGIN DELETE FROM deployment_targets WHERE revision=NEW.revision AND node_id=NEW.node_id; UPDATE config_revisions SET encrypted_state=x'' WHERE id=NEW.revision; END"); err != nil {
		t.Fatal(err)
	}
	p := &CanvasPublisher{Repository: r}
	result, err := p.PublishCanvas(context.Background(), "remove", "owner", batchSnapshots(states[:1]))
	if err == nil {
		t.Fatal("status query unexpectedly succeeded")
	}
	if len(result.Nodes) != 1 || result.Nodes[0].Revision == 0 || result.Nodes[0].NodeID != states[0].NodeID {
		t.Fatalf("lost queued revision: %+v", result)
	}
}

func TestCanvasRemovalCancellationWaitsForReceiptThenRollsBack(t *testing.T) {
	r, states, _ := batchFixture(t)
	previous := applySnapshot(t, r, states[0])
	p := &CanvasPublisher{Repository: r}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type response struct {
		result proxy.CanvasRemovePublication
		err    error
	}
	finished := make(chan response, 1)
	go func() {
		result, err := p.PublishCanvasRemoval(ctx, "remove", "owner", batchSnapshots(states[:1]))
		finished <- response{result, err}
	}()
	timeout := time.After(5 * time.Second)
	var revision int64
	for revision == 0 {
		select {
		case <-timeout:
			t.Fatal("no publication")
		case <-time.After(10 * time.Millisecond):
			pending, err := r.Pending(context.Background(), states[0].NodeID)
			if err != nil {
				t.Fatal(err)
			}
			if pending != nil {
				var state desiredstate.State
				if err = json.Unmarshal(pending.DesiredStateJson, &state); err != nil {
					t.Fatal(err)
				}
				revision = state.Revision
			}
		}
	}
	cancel()
	var publication proxy.CanvasRemovePublication
	select {
	case result := <-finished:
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("cancel error %v", result.err)
		}
		publication = result.result
	case <-timeout:
		t.Fatal("cancel did not return")
	}
	cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	rolled := make(chan error, 1)
	go func() { rolled <- p.RollbackCanvasRemoval(cleanup, publication) }()
	if err := r.Result(cleanup, states[0].NodeID, &pb.ApplyResult{Revision: revision, Success: true}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case err := <-rolled:
			if err != nil {
				t.Fatal(err)
			}
			current, err := r.Current(cleanup, states[0].NodeID)
			if err != nil || current != previous {
				t.Fatalf("current %d: %v", current, err)
			}
			return
		case <-cleanup.Done():
			t.Fatal(cleanup.Err())
		case <-time.After(10 * time.Millisecond):
			command, err := r.PendingRollback(cleanup, states[0].NodeID)
			if err != nil {
				t.Fatal(err)
			}
			if command != nil {
				if err = r.CommandResult(cleanup, states[0].NodeID, &pb.ApplyResult{Revision: command.Revision, CommandId: command.CommandId, Success: true}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestCanvasRemovalCancelOfflineQueuesRollbackWithoutWaiting(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			r, states, _ := batchFixture(t)
			p := &CanvasPublisher{Repository: r}
			ctx := context.Background()
			previous := map[string]int64{}
			for _, state := range states[:count] {
				previous[state.NodeID] = applySnapshot(t, r, state)
			}
			publication, err := p.PublishCanvas(ctx, "remove", "owner", batchSnapshots(states[:count]))
			if err != nil {
				t.Fatal(err)
			}
			result := proxy.CanvasRemovePublication{CanvasPublishResult: publication, Previous: previous}
			cancelCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			err = p.CancelCanvasRemoval(cancelCtx, result)
			if err == nil || !strings.Contains(err.Error(), "待确认") {
				t.Fatalf("cancel error %v", err)
			}
			if cancelCtx.Err() != nil {
				t.Fatal("cancel waited for offline node")
			}
			pending, err := r.Pending(ctx, states[0].NodeID)
			if err != nil || pending != nil {
				t.Fatalf("apply pending %+v %v", pending, err)
			}
			command, err := r.PendingRollback(ctx, states[0].NodeID)
			if err != nil || command == nil || command.Revision != previous[states[0].NodeID] {
				t.Fatalf("rollback %+v: %v", command, err)
			}
			if err = r.Result(ctx, states[0].NodeID, &pb.ApplyResult{Revision: publication.Nodes[0].Revision, Success: true}); err == nil {
				t.Fatal("late receipt accepted")
			}
			current, err := r.Current(ctx, states[0].NodeID)
			if err != nil || current != previous[states[0].NodeID] {
				t.Fatalf("late receipt changed runtime %d: %v", current, err)
			}
			if err = r.CommandResult(ctx, states[0].NodeID, &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Success: true}); err != nil {
				t.Fatal(err)
			}
			if count == 2 {
				batch, err := r.Batch(ctx, publication.BatchID)
				if err != nil || batch.Status != "rolled_back" || batch.Items[1].Status != "waiting" {
					t.Fatalf("batch %+v: %v", batch, err)
				}
			}
		})
	}
}

func TestCanvasRemovalCancelBatchRollsBackPendingThenEarlierSuccess(t *testing.T) {
	r, states, _ := batchFixture(t)
	ctx := context.Background()
	p := &CanvasPublisher{Repository: r}
	previous := map[string]int64{}
	for _, state := range states {
		previous[state.NodeID] = applySnapshot(t, r, state)
	}
	publication, err := p.PublishCanvas(ctx, "remove", "owner", batchSnapshots(states))
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Result(ctx, states[0].NodeID, &pb.ApplyResult{Revision: publication.Nodes[0].Revision, Success: true}); err != nil {
		t.Fatal(err)
	}
	if err = p.CancelCanvasRemoval(ctx, proxy.CanvasRemovePublication{CanvasPublishResult: publication, Previous: previous}); err == nil {
		t.Fatal("missing pending recovery result")
	}
	for _, index := range []int{1, 0} {
		command, err := r.PendingRollback(ctx, states[index].NodeID)
		if err != nil || command == nil || command.Revision != previous[states[index].NodeID] {
			t.Fatalf("rollback %d %+v: %v", index, command, err)
		}
		if err = r.CommandResult(ctx, states[index].NodeID, &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	batch, err := r.Batch(ctx, publication.BatchID)
	if err != nil || batch.Status != "rolled_back" || batch.Items[2].Status != "waiting" {
		t.Fatalf("batch %+v: %v", batch, err)
	}
}

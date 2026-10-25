package deployment

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
	"time"
)

// TestAppliedNotifiesOnlySuccessfulOperations records the hook the platform uses
// to re-check a node's user access surface: a deploy and a rollback re-publish a
// stored revision verbatim, so the only moment the panel knows the node's
// credentials are correct again is when the agent reports success.
func TestAppliedNotifiesOnlySuccessfulOperations(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	configurationVault, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Node A", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true},
		Credentials: []desiredstate.Credential{{ID: "credential", Protocol: "socks5", Username: "device", Secret: "private-device-password"}},
		Inbounds:    []desiredstate.Inbound{{ID: "entry", Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"credential"}}},
		Outbounds:   []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	applied := []string{}
	repository := &Repository{Database: database, Vault: configurationVault, Applied: func(nodeID, operation string) {
		if nodeID != n.ID {
			t.Errorf("the hook named node %q instead of %q", nodeID, n.ID)
		}
		applied = append(applied, operation)
	}}
	revision, err := repository.Create(ctx, state, "owner", "User access")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Result(ctx, n.ID, &pb.ApplyResult{Revision: revision.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != "post-deploy" {
		t.Fatalf("a successful deploy notified %v", applied)
	}
	// A repeated result is not a second application.
	if err := repository.Result(ctx, n.ID, &pb.ApplyResult{Revision: revision.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 {
		t.Fatalf("a repeated result notified %v", applied)
	}
	// A rollback re-publishes a stored revision, so its success is the other
	// moment the node's credentials have to be re-checked.
	command, err := repository.RollbackNode(ctx, n.ID, revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Result(ctx, n.ID, &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Success: true}); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 || applied[1] != "post-rollback" {
		t.Fatalf("a successful rollback notified %v", applied)
	}
	failed, err := repository.RollbackNode(ctx, n.ID, revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Result(ctx, n.ID, &pb.ApplyResult{CommandId: failed.CommandId, Revision: failed.Revision, Code: "rollback_failed"}); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("a failed rollback notified %v", applied)
	}
}

// TestBatchRollbackNotifiesThePlatform covers the batch paths: a batch deploy
// reports one notification per node, and every batch rollback command reports
// one more.
func TestBatchRollbackNotifiesThePlatform(t *testing.T) {
	ctx := context.Background()
	r, states, _ := batchFixture(t)
	applied := []string{}
	r.Applied = func(nodeID, operation string) { applied = append(applied, nodeID+":"+operation) }
	batch, err := r.CreateBatch(ctx, "chain", "publication", "owner", states)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.StartBatch(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	for _, item := range batch.Items {
		if err := r.Result(ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	if len(applied) != len(batch.Items) {
		t.Fatalf("a batch deploy notified %v", applied)
	}
	for index, item := range batch.Items {
		if applied[index] != item.NodeID+":post-deploy" {
			t.Fatalf("batch deploy notification %d: %q", index, applied[index])
		}
	}
	if err := r.RollbackBatch(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	applied = nil
	for index := len(batch.Items) - 1; index >= 0; index-- {
		item := batch.Items[index]
		command, err := r.PendingRollback(ctx, item.NodeID)
		if err != nil || command == nil {
			t.Fatalf("missing rollback command for %s: %v", item.NodeID, err)
		}
		if err := r.Result(ctx, item.NodeID, &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	if len(applied) != len(batch.Items) {
		t.Fatalf("a batch rollback notified %v", applied)
	}
	for _, notification := range applied {
		if !strings.HasSuffix(notification, ":post-rollback") {
			t.Fatalf("batch rollback notification: %q", notification)
		}
	}
}

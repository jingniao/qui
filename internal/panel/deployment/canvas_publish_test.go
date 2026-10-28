package deployment

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
)

// canvasState is the smallest state the deployment module accepts for a node.
func canvasState(nodeID string, port int) desiredstate.State {
	return desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: nodeID, Logging: desiredstate.Logging{DomainOnly: true}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Enabled: true, Listen: "127.0.0.1", Port: port, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}}
}

// TestCanvasPublisherUsesRevisionOrBatch checks both publication shapes a canvas
// scheme can need: a single touched node becomes one deployed revision, and two
// or more nodes become one started batch whose items keep the order they were
// captured in (targets first).
func TestCanvasPublisherUsesRevisionOrBatch(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	first, _, _, err := nodes.CreateEnrollment(ctx, "Exit", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := nodes.CreateEnrollment(ctx, "Entry", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	lone, _, _, err := nodes.CreateEnrollment(ctx, "Lone", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &CanvasPublisher{Repository: &Repository{Database: database, Vault: key}}
	single, err := publisher.PublishCanvas(ctx, "方案发布 single", "owner", []proxy.NodeSnapshot{{State: canvasState(lone.ID, 2100)}})
	if err != nil {
		t.Fatal(err)
	}
	if single.BatchID != "" || len(single.Nodes) != 1 || single.Nodes[0].Status != "pending" || single.Nodes[0].Revision == 0 {
		t.Fatalf("single-node publication %+v", single)
	}
	queued, err := publisher.PublishCanvas(ctx, "方案发布 batch", "owner", []proxy.NodeSnapshot{{State: canvasState(first.ID, 2200)}, {State: canvasState(second.ID, 2300)}})
	if err != nil {
		t.Fatal(err)
	}
	if queued.BatchID == "" || len(queued.Nodes) != 2 || queued.Nodes[0].NodeID != first.ID || queued.Nodes[1].NodeID != second.ID {
		t.Fatalf("batch publication %+v", queued)
	}
	batch, err := publisher.Repository.Batch(ctx, queued.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 2 || batch.Items[0].NodeID != first.ID || batch.Status != "deploying" {
		t.Fatalf("batch %+v", batch)
	}
}

// TestCanvasPublisherSingleNodeChecksRelayTargets checks that a one-node canvas
// publication obeys the dependency rule of every other single-node path: a relay
// outbound whose target has published nothing yet is refused before a revision
// is created.
func TestCanvasPublisherSingleNodeChecksRelayTargets(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	target, _, _, err := nodes.CreateEnrollment(ctx, "Target", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	source, _, _, err := nodes.CreateEnrollment(ctx, "Source", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	state := canvasState(source.ID, 2400)
	state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: "relay", Protocol: "trojan", Enabled: true, TargetNodeID: target.ID})
	publisher := &CanvasPublisher{Repository: &Repository{Database: database, Vault: key}}
	if _, err := publisher.PublishCanvas(ctx, "方案发布 relay", "owner", []proxy.NodeSnapshot{{State: state}}); err == nil {
		t.Fatal("a relay outbound to an unpublished target was deployed")
	}
	var revisions int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM config_revisions").Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 0 {
		t.Fatalf("a refused publication left %d revisions", revisions)
	}
}

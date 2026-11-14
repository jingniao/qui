package proxy

import (
	"context"
	"errors"
	"testing"

	"proxycontrol/internal/panel/node"
)

func TestOutboundSourceSnapshotGate(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.nodes.Database.Exec(`UPDATE nodes SET capabilities_json='{"coreName":"xray"}' WHERE id=?`, f.nodeA); err != nil {
		t.Fatal(err)
	}
	input := OutboundInput{Name: "bound", NodeID: f.nodeA, Protocol: "direct", Enabled: true, SupportTCP: true, SourceIP: "192.0.2.8"}
	if _, err := f.service.SaveOutbound(ctx, "", input); !errors.Is(err, node.ErrOutboundSourceUnsupported) {
		t.Fatalf("save: %v", err)
	}
	if _, err := f.service.Repository.SaveOutbound(ctx, "", input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Snapshot(ctx, f.nodeA, f.users); !errors.Is(err, node.ErrOutboundSourceUnsupported) {
		t.Fatalf("snapshot: %v", err)
	}
}

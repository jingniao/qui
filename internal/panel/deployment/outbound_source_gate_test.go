package deployment

import (
	"context"
	"errors"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestOutboundSourcePublishGate(t *testing.T) {
	r, _, id, _ := coreNodeFixture(t, "linux/amd64", "xray")
	setMultiCore(t, r, id, true)
	tx, err := r.Database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	state := desiredstate.State{NodeID: id, Outbounds: []desiredstate.Outbound{{ID: "out", Enabled: true, SourceIP: "192.0.2.8"}}}
	if err := validateStateCoresTx(context.Background(), tx, state); !errors.Is(err, node.ErrOutboundSourceUnsupported) {
		t.Fatalf("publish: %v", err)
	}
}

package deployment

import (
	"context"
	"proxycontrol/internal/panel/proxy"
	"testing"
)

// TestBatchRevisionRecordsInboundIntent covers the multi-node publication path.
// A node whose last publication is a chain or relay batch must record the
// Owner's inbound intent exactly like a single-node publication, otherwise the
// batch would leave the column NULL and an automatic quota revision could never
// re-enable an inbound that publication emptied.
func TestBatchRevisionRecordsInboundIntent(t *testing.T) {
	ctx := context.Background()
	r, states, _ := batchFixture(t)
	// The renderer writes enabled=false for an inbound that no user credential
	// reaches. The Owner still wants that inbound enabled: the batch has to keep
	// that intent next to the state it disables.
	states[0].Inbounds[0].Enabled = false
	enabled := states[0].Inbounds[0].ID
	batch, err := r.CreateBatch(ctx, "chain", "intent publication", "owner", []proxy.NodeSnapshot{
		{State: states[0], InboundIntent: []string{enabled}},
		{State: states[1], InboundIntent: []string{}},
		{State: states[2]},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := r.InboundIntent(ctx, batch.Items[0].Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(intent) != 1 || intent[0] != enabled {
		t.Fatalf("the batch revision recorded the intent %v instead of [%s]", intent, enabled)
	}
	// An explicit empty set is not the same as an unknown one: it tells an
	// automatic quota revision that the Owner wants nothing enabled.
	intent, err = r.InboundIntent(ctx, batch.Items[1].Revision)
	if err != nil {
		t.Fatal(err)
	}
	if intent == nil || len(intent) != 0 {
		t.Fatalf("an empty intent was recorded as %v", intent)
	}
	// A snapshot without an intent records none, which stays "unknown".
	intent, err = r.InboundIntent(ctx, batch.Items[2].Revision)
	if err != nil {
		t.Fatal(err)
	}
	if intent != nil {
		t.Fatalf("a snapshot without an intent recorded %v", intent)
	}
}

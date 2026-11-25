package proxy

import (
	"context"
	"testing"
)

// TestInboundDraftCarriesEgressSourceIP: the publish-state comparison reads the
// draft from InboundDraft, so a draft without the stored egress IP made every
// inbound with one look modified after a successful publish.
func TestInboundDraftCarriesEgressSourceIP(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	inbound, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "Egress", NodeID: f.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Repository.Database.Exec("UPDATE inbounds SET egress_source_ip='2001:db8::10' WHERE id=?", inbound.ID); err != nil {
		t.Fatal(err)
	}
	draft, enabled, err := f.service.InboundDraft(ctx, f.nodeA)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Inbounds) != 1 || draft.Inbounds[0].ID != inbound.ID || draft.Inbounds[0].EgressSourceIP != "2001:db8::10" {
		t.Fatalf("draft lost the egress IP: %+v", draft.Inbounds)
	}
	if len(enabled) != 1 || enabled[0] != inbound.ID {
		t.Fatalf("enabled intent = %v", enabled)
	}
}

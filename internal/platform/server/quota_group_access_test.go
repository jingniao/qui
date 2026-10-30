package server

import (
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestQuotaMergeDoesNotRetainBindingToRemovedInbound(t *testing.T) {
	c := desiredstate.Credential{ID: "shared", UserID: "u", Protocol: "vless"}
	a := desiredstate.Inbound{ID: "a", Protocol: "vless", Enabled: true, CredentialIDs: []string{c.ID}}
	b := a
	b.ID = "b"
	published := desiredstate.State{Credentials: []desiredstate.Credential{c}, Inbounds: []desiredstate.Inbound{a, b}}
	fresh := published
	fresh.Inbounds = []desiredstate.Inbound{b}
	next := mergeUserAccess(published, fresh, []string{"a", "b"})
	if len(next.Inbounds[0].CredentialIDs) != 0 || next.Inbounds[0].Enabled || len(next.Inbounds[1].CredentialIDs) != 1 {
		t.Fatal("removed inbound kept a shared credential", next.Inbounds)
	}
}

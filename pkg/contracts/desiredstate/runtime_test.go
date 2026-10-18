package desiredstate

import (
	"encoding/json"
	"testing"
	"time"
)

func TestExpirationRemovesOnlyExpiredAccountsAndKeepsSnapshotImmutable(t *testing.T) {
	now := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	state := State{Users: []User{{ID: "expired", Enabled: true, ExpiresAt: now.Format(time.RFC3339)}, {ID: "active", Enabled: true, ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}}, Credentials: []Credential{{ID: "old", UserID: "expired"}, {ID: "live", UserID: "active"}, {ID: "relay", Internal: true}}, Inbounds: []Inbound{{ID: "mixed", Enabled: true, CredentialIDs: []string{"old", "live"}}, {ID: "old-only", Enabled: true, CredentialIDs: []string{"old"}}, {ID: "local-no-auth", Enabled: true}, {ID: "internal", Enabled: true, CredentialIDs: []string{"relay"}}}}
	before, _ := json.Marshal(state)
	derived, err := state.RuntimeAt(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(derived.Credentials) != 2 || !derived.Inbounds[0].Enabled || len(derived.Inbounds[0].CredentialIDs) != 1 || derived.Inbounds[1].Enabled || !derived.Inbounds[2].Enabled || !derived.Inbounds[3].Enabled {
		t.Fatal("expiration changed unrelated listeners", derived)
	}
	after, _ := json.Marshal(state)
	if string(before) != string(after) {
		t.Fatal("immutable snapshot mutated")
	}
	beforeDeadline, err := state.RuntimeAt(now.Add(-time.Nanosecond))
	if err != nil || !beforeDeadline.Inbounds[1].Enabled {
		t.Fatal("expiration boundary early")
	}
}
func TestExpirationDropsWireGuardPeerWithoutMutatingOriginal(t *testing.T) {
	now := time.Now().UTC()
	state := State{Users: []User{{ID: "expired", Enabled: true, ExpiresAt: now.Format(time.RFC3339Nano)}}, Credentials: []Credential{{ID: "expired-peer", UserID: "expired"}, {ID: "live-peer", Internal: true}}, Inbounds: []Inbound{{Enabled: true, CredentialIDs: []string{"expired-peer", "live-peer"}, Settings: ProtocolSettings{WireGuard: &WireGuardSettings{Addresses: []string{"10.0.0.1/24"}, Peers: []WireGuardPeer{{CredentialID: "expired-peer", AllowedIPs: []string{"10.0.0.2/32"}}, {CredentialID: "live-peer", AllowedIPs: []string{"10.0.0.3/32"}}}}}}}}
	derived, err := state.RuntimeAt(now)
	if err != nil || len(derived.Inbounds[0].Settings.WireGuard.Peers) != 1 || derived.Inbounds[0].Settings.WireGuard.Peers[0].CredentialID != "live-peer" {
		t.Fatal(derived, err)
	}
	if len(state.Inbounds[0].Settings.WireGuard.Peers) != 2 {
		t.Fatal("original peers changed")
	}
}

func TestExpirationValidationRejectsInvalidDeadline(t *testing.T) {
	state := State{SchemaVersion: 1, Revision: 1, NodeID: "node", Logging: Logging{DomainOnly: true}, Users: []User{{ID: "user", Enabled: true, ExpiresAt: "not-a-time"}}, Outbounds: []Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	if err := state.Validate(); err == nil {
		t.Fatal("invalid expiration accepted")
	}
	state.Users[0].ExpiresAt = "2026-10-02T18:00:00+08:00"
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	derived, err := state.RuntimeAt(time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC))
	if err != nil || derived.Users[0].Enabled {
		t.Fatal("offset deadline boundary incorrect")
	}
}

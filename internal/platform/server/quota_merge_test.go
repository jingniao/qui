package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
)

// mergeState builds the smallest published/fresh pair that exercises the
// user-access merge: one VLESS inbound that is enabled and bound to the user's
// credential.
func mergedCredentialFixture() (desiredstate.State, desiredstate.State) {
	published := desiredstate.State{
		SchemaVersion: 1,
		Revision:      4,
		NodeID:        "node-1",
		Logging:       desiredstate.Logging{DomainOnly: true},
		Users:         []desiredstate.User{{ID: "user-1", Enabled: true}},
		Credentials:   []desiredstate.Credential{{ID: "cred-1", UserID: "user-1", Protocol: "vless", Secret: "secret"}},
		Inbounds: []desiredstate.Inbound{{
			ID: "inbound-1", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: true,
			CredentialIDs: []string{"cred-1"}, DefaultOutbound: "direct",
		}},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}},
		Policies:  []desiredstate.Policy{},
	}
	fresh := published
	fresh.Revision = 9
	return published, fresh
}

// wgKey returns a deterministic 32-byte WireGuard key for the fixtures below;
// validation only checks the base64 encoding and the length.
func wgKey(seed byte) string {
	raw := make([]byte, 32)
	for index := range raw {
		raw[index] = seed + byte(index)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// mergedWireGuardFixture builds the smallest published/fresh pair around one
// shared WireGuard inbound: two users, one literal-key peer and one peer bound
// to an internal relay credential.
func mergedWireGuardFixture() (desiredstate.State, desiredstate.State) {
	published := desiredstate.State{
		SchemaVersion: 1,
		Revision:      4,
		NodeID:        "node-1",
		Logging:       desiredstate.Logging{DomainOnly: true},
		Users:         []desiredstate.User{{ID: "user-1", Enabled: true}, {ID: "user-2", Enabled: true}},
		Credentials: []desiredstate.Credential{
			{ID: "wg-1", UserID: "user-1", Protocol: "wireguard", PublicKey: wgKey(11)},
			{ID: "wg-2", UserID: "user-2", Protocol: "wireguard", PublicKey: wgKey(12)},
			{ID: "relay", Protocol: "wireguard", Internal: true, Secret: wgKey(31)},
		},
		Inbounds: []desiredstate.Inbound{{
			ID: "wg-in", Protocol: "wireguard", Listen: "0.0.0.0", Port: 51820, Enabled: true,
			CredentialIDs: []string{"wg-1", "wg-2", "relay"}, DefaultOutbound: "direct",
			Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{
				SecretKey: wgKey(1), Addresses: []string{"10.99.0.1/24"},
				Peers: []desiredstate.WireGuardPeer{
					{PublicKey: wgKey(21), AllowedIPs: []string{"10.99.0.9/32"}},
					{PublicKey: wgKey(22), AllowedIPs: []string{"10.99.0.10/32"}, CredentialID: "relay"},
					{PublicKey: wgKey(11), AllowedIPs: []string{"10.99.0.2/32"}, CredentialID: "wg-1"},
					{PublicKey: wgKey(12), AllowedIPs: []string{"10.99.0.3/32"}, CredentialID: "wg-2"},
				},
			}},
		}},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}},
		Policies:  []desiredstate.Policy{},
	}
	fresh := published
	fresh.Revision = 9
	return published, fresh
}

// peersWithout returns a peer list without the peer bound to one credential,
// which is what the renderer emits once that credential is gone.
func peersWithout(peers []desiredstate.WireGuardPeer, credentialID string) []desiredstate.WireGuardPeer {
	kept := []desiredstate.WireGuardPeer{}
	for _, peer := range peers {
		if peer.CredentialID != credentialID {
			kept = append(kept, peer)
		}
	}
	return kept
}

func peerForCredential(inbound desiredstate.Inbound, credentialID string) (desiredstate.WireGuardPeer, bool) {
	for _, peer := range inbound.Settings.WireGuard.Peers {
		if peer.CredentialID == credentialID {
			return peer, true
		}
	}
	return desiredstate.WireGuardPeer{}, false
}

func credentialForTest(state desiredstate.State, credentialID string) (desiredstate.Credential, bool) {
	for _, credential := range state.Credentials {
		if credential.ID == credentialID {
			return credential, true
		}
	}
	return desiredstate.Credential{}, false
}

func TestMergeUserAccessRemoval(t *testing.T) {
	published, fresh := mergedCredentialFixture()
	fresh.Users = []desiredstate.User{}
	fresh.Credentials = []desiredstate.Credential{}
	fresh.Inbounds = []desiredstate.Inbound{{
		ID: "inbound-1", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: false,
		CredentialIDs: []string{}, DefaultOutbound: "direct",
	}}

	next := mergeUserAccess(published, fresh, nil)
	if len(next.Users) != 0 || len(next.Credentials) != 0 {
		t.Fatalf("removed user survived: users=%d credentials=%d", len(next.Users), len(next.Credentials))
	}
	if len(next.Inbounds) != 1 || len(next.Inbounds[0].CredentialIDs) != 0 || next.Inbounds[0].Enabled {
		t.Fatalf("inbound kept a credential: %+v", next.Inbounds)
	}
	if quotaStatesEqual(published, next) {
		t.Fatal("a removal must produce a different state")
	}
}

func TestMergeUserAccessReactivation(t *testing.T) {
	published, fresh := mergedCredentialFixture()
	published.Users = []desiredstate.User{}
	published.Credentials = []desiredstate.Credential{}
	published.Inbounds = []desiredstate.Inbound{{
		ID: "inbound-1", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: false,
		CredentialIDs: []string{}, DefaultOutbound: "direct",
	}}

	next := mergeUserAccess(published, fresh, []string{"inbound-1"})
	if len(next.Users) != 1 || next.Users[0].ID != "user-1" {
		t.Fatalf("reactivated user missing: %+v", next.Users)
	}
	if len(next.Credentials) != 1 || next.Credentials[0].ID != "cred-1" {
		t.Fatalf("reactivated credential missing: %+v", next.Credentials)
	}
	inbound := next.Inbounds[0]
	if !inbound.Enabled || len(inbound.CredentialIDs) != 1 || inbound.CredentialIDs[0] != "cred-1" {
		t.Fatalf("inbound not re-enabled with the credential: %+v", inbound)
	}
}

// TestMergeUserAccessMatchesProtocol ensures a reactivated credential is bound
// only to the inbound whose protocol it matches.
func TestMergeUserAccessMatchesProtocol(t *testing.T) {
	published := desiredstate.State{
		SchemaVersion: 1, Revision: 4, NodeID: "node-1",
		Logging:     desiredstate.Logging{DomainOnly: true},
		Users:       []desiredstate.User{},
		Credentials: []desiredstate.Credential{},
		Inbounds: []desiredstate.Inbound{
			{ID: "vless-in", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: false, CredentialIDs: []string{}, DefaultOutbound: "direct"},
			{ID: "socks-in", Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: false, CredentialIDs: []string{}, DefaultOutbound: "direct"},
		},
	}
	fresh := published
	fresh.Revision = 9
	fresh.Users = []desiredstate.User{{ID: "user-1", Enabled: true}}
	fresh.Credentials = []desiredstate.Credential{{ID: "cred-1", UserID: "user-1", Protocol: "vless", Secret: "secret"}}
	fresh.Inbounds = []desiredstate.Inbound{
		{ID: "vless-in", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: true, CredentialIDs: []string{"cred-1"}, DefaultOutbound: "direct"},
		{ID: "socks-in", Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: false, CredentialIDs: []string{}, DefaultOutbound: "direct"},
	}

	next := mergeUserAccess(published, fresh, []string{"vless-in"})
	if next.Inbounds[0].Enabled != true || len(next.Inbounds[0].CredentialIDs) != 1 {
		t.Fatalf("matching inbound not restored: %+v", next.Inbounds[0])
	}
	if next.Inbounds[1].Enabled || len(next.Inbounds[1].CredentialIDs) != 0 {
		t.Fatalf("credential leaked to a mismatched inbound: %+v", next.Inbounds[1])
	}
}

// TestMergeUserAccessInboundAbsentFromFresh keeps only the published credential
// IDs that still exist in the fresh credential set when the inbound is gone.
func TestMergeUserAccessInboundAbsentFromFresh(t *testing.T) {
	published := desiredstate.State{
		SchemaVersion: 1, Revision: 4, NodeID: "node-1",
		Logging: desiredstate.Logging{DomainOnly: true},
		Users:   []desiredstate.User{{ID: "user-1", Enabled: true}},
		Credentials: []desiredstate.Credential{
			{ID: "cred-1", UserID: "user-1", Protocol: "vless", Secret: "one"},
			{ID: "cred-2", UserID: "user-1", Protocol: "vless", Secret: "two"},
		},
		Inbounds: []desiredstate.Inbound{{
			ID: "inbound-1", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: true,
			CredentialIDs: []string{"cred-1", "cred-2"}, DefaultOutbound: "direct",
		}},
	}
	fresh := published
	fresh.Revision = 9
	fresh.Inbounds = []desiredstate.Inbound{}
	fresh.Credentials = []desiredstate.Credential{{ID: "cred-2", UserID: "user-1", Protocol: "vless", Secret: "two"}}

	next := mergeUserAccess(published, fresh, nil)
	if len(next.Credentials) != 1 || next.Credentials[0].ID != "cred-2" {
		t.Fatalf("credentials were not replaced by the fresh view: %+v", next.Credentials)
	}
	got := next.Inbounds[0].CredentialIDs
	if len(got) != 1 || got[0] != "cred-2" {
		t.Fatalf("stale inbound credential not pruned: %+v", got)
	}
}

// TestMergeUserAccessDropsAbsentInboundWhenAllCredentialsGone verifies the
// renderer's disable-when-empty rule is mirrored for an inbound missing from the
// fresh state once all of its credentials have disappeared.
func TestMergeUserAccessDropsAbsentInboundWhenAllCredentialsGone(t *testing.T) {
	published, fresh := mergedCredentialFixture()
	fresh.Users = []desiredstate.User{}
	fresh.Credentials = []desiredstate.Credential{}
	fresh.Inbounds = []desiredstate.Inbound{}

	next := mergeUserAccess(published, fresh, nil)
	if next.Inbounds[0].Enabled || len(next.Inbounds[0].CredentialIDs) != 0 {
		t.Fatalf("absent inbound was not disabled: %+v", next.Inbounds[0])
	}
}

// TestMergeUserAccessTakesFreshUserEntries pins the rule that the user slice is
// the fresh view's: renewing a still-active user must change the merged
// ExpiresAt even though the credential set is identical, because the agent
// enforces the deadline from this entry itself.
func TestMergeUserAccessTakesFreshUserEntries(t *testing.T) {
	published, fresh := mergedCredentialFixture()
	published.Users = []desiredstate.User{{ID: "user-1", Enabled: true, ExpiresAt: "2026-11-01T00:00:00Z"}}
	fresh.Users = []desiredstate.User{{ID: "user-1", Enabled: true, ExpiresAt: "2026-12-01T00:00:00Z"}}

	next := mergeUserAccess(published, fresh, nil)
	if len(next.Users) != 1 || next.Users[0].ExpiresAt != "2026-12-01T00:00:00Z" {
		t.Fatalf("renewal did not reach the merged users: %+v", next.Users)
	}
	if quotaStatesEqual(published, next) {
		t.Fatal("a renewed expiry must produce a different state")
	}
}

// TestMergeUserAccessNoOpEquality proves that an identical user surface yields a
// state equal to the published one even when the fresh render carries
// unreviewed business changes: those changes must never be merged.
func TestMergeUserAccessNoOpEquality(t *testing.T) {
	published, fresh := mergedCredentialFixture()
	fresh.Outbounds = append(append([]desiredstate.Outbound(nil), published.Outbounds...), desiredstate.Outbound{ID: "draft-exit", Protocol: "direct", Enabled: true})
	fresh.Policies = append([]desiredstate.Policy(nil), desiredstate.Policy{ID: "draft-policy", Priority: 50, Enabled: true})
	fresh.ProxyChains = []desiredstate.ProxyChain{{ID: "draft-chain"}}

	next := mergeUserAccess(published, fresh, nil)
	if !quotaStatesEqual(published, next) {
		t.Fatal("an unchanged user surface must equal the published state")
	}
	if len(next.Outbounds) != len(published.Outbounds) || len(next.Policies) != len(published.Policies) || len(next.ProxyChains) != 0 {
		t.Fatalf("unpublished business config leaked: outbounds=%d policies=%d chains=%d", len(next.Outbounds), len(next.Policies), len(next.ProxyChains))
	}
}

// TestMergeUserAccessKeepsPublishedInternalCredential pins that an unpublished
// edit of an outbound credential cannot strip the credential a published
// outbound still references: the automatic revision would otherwise fail
// validation on the node's own published outbound.
func TestMergeUserAccessKeepsPublishedInternalCredential(t *testing.T) {
	published := desiredstate.State{
		SchemaVersion: 1, Revision: 4, NodeID: "node-1",
		Logging:     desiredstate.Logging{DomainOnly: true},
		Users:       []desiredstate.User{},
		Credentials: []desiredstate.Credential{{ID: "exit-cred", Protocol: "socks5", Username: "relay", Secret: "published-secret", Internal: true}},
		Inbounds:    []desiredstate.Inbound{},
		Outbounds: []desiredstate.Outbound{
			{ID: "direct", Protocol: "direct", Enabled: true},
			{ID: "exit", Protocol: "socks5", Address: "203.0.113.9", Port: 1080, Enabled: true, CredentialID: "exit-cred"},
		},
		Policies: []desiredstate.Policy{},
	}
	if err := published.Validate(); err != nil {
		t.Fatalf("fixture is not valid: %v", err)
	}
	fresh := published
	fresh.Revision = 9
	// The Owner replaced the outbound's credential in the draft and has not
	// published that edit.
	fresh.Credentials = []desiredstate.Credential{{ID: "draft-cred", Protocol: "socks5", Username: "relay", Secret: "draft-secret", Internal: true}}
	fresh.Outbounds = append([]desiredstate.Outbound(nil), published.Outbounds...)
	fresh.Outbounds[1].CredentialID = "draft-cred"

	next := mergeUserAccess(published, fresh, nil)
	if len(next.Credentials) != 1 || next.Credentials[0].ID != "exit-cred" || next.Credentials[0].Secret != "published-secret" {
		t.Fatalf("published outbound credential was not kept as published: %+v", next.Credentials)
	}
	if len(next.Outbounds) != 2 || next.Outbounds[1].CredentialID != "exit-cred" {
		t.Fatalf("outbound configuration changed: %+v", next.Outbounds)
	}
	if err := next.Validate(); err != nil {
		t.Fatalf("merged state does not validate: %v", err)
	}
}

// TestMergeUserAccessDropsInactiveWireGuardPeer covers a shared WireGuard
// inbound: the inactive user's peer has to disappear together with its
// credential, or the core refuses to render the inbound ("WireGuard peer
// credential missing"), and reactivation has to bring the peer back.
func TestMergeUserAccessDropsInactiveWireGuardPeer(t *testing.T) {
	published, full := mergedWireGuardFixture()
	fresh := full
	fresh.Users = []desiredstate.User{{ID: "user-1", Enabled: true}}
	fresh.Credentials = []desiredstate.Credential{published.Credentials[0], published.Credentials[2]}
	inbound := published.Inbounds[0]
	inbound.CredentialIDs = []string{"wg-1", "relay"}
	settings := *inbound.Settings.WireGuard
	settings.Peers = peersWithout(published.Inbounds[0].Settings.WireGuard.Peers, "wg-2")
	inbound.Settings.WireGuard = &settings
	fresh.Inbounds = []desiredstate.Inbound{inbound}

	next := mergeUserAccess(published, fresh, nil)
	if err := next.Validate(); err != nil {
		t.Fatalf("merged state does not validate: %v", err)
	}
	if _, exists := credentialForTest(next, "wg-2"); exists {
		t.Fatalf("inactive user's credential survived: %+v", next.Credentials)
	}
	if len(next.Inbounds[0].CredentialIDs) != 2 {
		t.Fatalf("inactive user's credential is still bound: %+v", next.Inbounds[0].CredentialIDs)
	}
	if _, exists := peerForCredential(next.Inbounds[0], "wg-2"); exists {
		t.Fatalf("inactive user's peer survived: %+v", next.Inbounds[0].Settings.WireGuard.Peers)
	}
	if _, exists := peerForCredential(next.Inbounds[0], "wg-1"); !exists {
		t.Fatalf("active user's peer disappeared: %+v", next.Inbounds[0].Settings.WireGuard.Peers)
	}

	// Reactivation: the credential is back in the draft, so the peer returns.
	restored := mergeUserAccess(next, full, []string{"wg-in"})
	if err := restored.Validate(); err != nil {
		t.Fatalf("reactivated state does not validate: %v", err)
	}
	if _, exists := credentialForTest(restored, "wg-2"); !exists {
		t.Fatalf("reactivated credential missing: %+v", restored.Credentials)
	}
	if _, exists := peerForCredential(restored.Inbounds[0], "wg-2"); !exists {
		t.Fatalf("reactivated peer missing: %+v", restored.Inbounds[0].Settings.WireGuard.Peers)
	}
}

// TestMergeUserAccessWireGuardKeyRotation pins that a rotated device key from
// the draft replaces the published one, on the credential and on its peer.
func TestMergeUserAccessWireGuardKeyRotation(t *testing.T) {
	published, fresh := mergedWireGuardFixture()
	rotated := wgKey(41)
	fresh.Credentials = []desiredstate.Credential{published.Credentials[0], published.Credentials[1], published.Credentials[2]}
	fresh.Credentials[0].PublicKey = rotated
	inbound := fresh.Inbounds[0]
	settings := *inbound.Settings.WireGuard
	settings.Peers = append([]desiredstate.WireGuardPeer(nil), settings.Peers...)
	for index := range settings.Peers {
		if settings.Peers[index].CredentialID == "wg-1" {
			settings.Peers[index].PublicKey = rotated
		}
	}
	inbound.Settings.WireGuard = &settings
	fresh.Inbounds = []desiredstate.Inbound{inbound}

	next := mergeUserAccess(published, fresh, []string{"wg-in"})
	if err := next.Validate(); err != nil {
		t.Fatalf("merged state does not validate: %v", err)
	}
	credential, exists := credentialForTest(next, "wg-1")
	if !exists || credential.PublicKey != rotated {
		t.Fatalf("rotated credential not picked up: %+v", credential)
	}
	peer, exists := peerForCredential(next.Inbounds[0], "wg-1")
	if !exists || peer.PublicKey != rotated {
		t.Fatalf("rotated peer key not picked up: %+v", peer)
	}
	if peer, exists := peerForCredential(next.Inbounds[0], "wg-2"); !exists || peer.PublicKey != wgKey(12) {
		t.Fatalf("untouched peer changed: %+v", peer)
	}
}

// TestMergeUserAccessKeepsLiteralAndInternalPeers checks the two peer kinds that
// are not user bindings: a literal-key peer and a peer bound to an internal
// relay credential survive a user change, and an unpublished edit of the relay
// credential does not ride along.
func TestMergeUserAccessKeepsLiteralAndInternalPeers(t *testing.T) {
	published, fresh := mergedWireGuardFixture()
	fresh.Users = []desiredstate.User{{ID: "user-1", Enabled: true}}
	fresh.Credentials = []desiredstate.Credential{
		published.Credentials[0],
		{ID: "relay", Protocol: "wireguard", Internal: true, Secret: wgKey(32)},
	}
	inbound := published.Inbounds[0]
	inbound.CredentialIDs = []string{"wg-1", "relay"}
	settings := *inbound.Settings.WireGuard
	settings.Peers = peersWithout(published.Inbounds[0].Settings.WireGuard.Peers, "wg-2")
	inbound.Settings.WireGuard = &settings
	fresh.Inbounds = []desiredstate.Inbound{inbound}

	next := mergeUserAccess(published, fresh, nil)
	if err := next.Validate(); err != nil {
		t.Fatalf("merged state does not validate: %v", err)
	}
	if _, exists := peerForCredential(next.Inbounds[0], ""); !exists {
		t.Fatalf("literal-key peer disappeared: %+v", next.Inbounds[0].Settings.WireGuard.Peers)
	}
	peer, exists := peerForCredential(next.Inbounds[0], "relay")
	if !exists || peer.PublicKey != wgKey(22) {
		t.Fatalf("internal-credential peer changed: %+v", peer)
	}
	credential, exists := credentialForTest(next, "relay")
	if !exists || credential.Secret != wgKey(31) {
		t.Fatalf("unpublished relay credential edit leaked: %+v", credential)
	}
}

// TestMergeUserAccessKeepsDisabledInboundDisabled pins the re-enable rule: an
// inbound the Owner disabled by hand stays disabled even when the draft assigns
// credentials to it and enables it again. The published revision recorded that
// the Owner wanted no inbound enabled, which is the only thing that may re-enable
// one.
func TestMergeUserAccessKeepsDisabledInboundDisabled(t *testing.T) {
	published := desiredstate.State{
		SchemaVersion: 1, Revision: 4, NodeID: "node-1",
		Logging:     desiredstate.Logging{DomainOnly: true},
		Users:       []desiredstate.User{},
		Credentials: []desiredstate.Credential{},
		Inbounds: []desiredstate.Inbound{{
			ID: "inbound-1", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: false,
			CredentialIDs: []string{}, DefaultOutbound: "direct",
		}},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}},
		Policies:  []desiredstate.Policy{},
	}
	fresh := published
	fresh.Revision = 9
	fresh.Users = []desiredstate.User{{ID: "user-1", Enabled: true}}
	fresh.Credentials = []desiredstate.Credential{{ID: "cred-1", UserID: "user-1", Protocol: "vless", Secret: "secret"}}
	fresh.Inbounds = []desiredstate.Inbound{{
		ID: "inbound-1", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: true,
		CredentialIDs: []string{"cred-1"}, DefaultOutbound: "direct",
	}}

	next := mergeUserAccess(published, fresh, []string{})
	if next.Inbounds[0].Enabled {
		t.Fatalf("administratively disabled inbound was re-enabled: %+v", next.Inbounds[0])
	}
	if len(next.Inbounds[0].CredentialIDs) != 1 || next.Inbounds[0].CredentialIDs[0] != "cred-1" {
		t.Fatalf("credential was not bound: %+v", next.Inbounds[0].CredentialIDs)
	}
	if err := next.Validate(); err != nil {
		t.Fatalf("merged state does not validate: %v", err)
	}
}

// TestMergeUserAccessUnknownIntentNeverReenables covers a revision published
// before the intent was recorded (NULL reads back as no intent) and one whose
// recorded intent enables no inbound: both keep a disabled inbound off, because
// an automatic revision must not undo an administrative disable it cannot rule
// out. The credential is still attached, so the merge produces what the node can
// run.
func TestMergeUserAccessUnknownIntentNeverReenables(t *testing.T) {
	published, fresh := mergedCredentialFixture()
	published.Users = []desiredstate.User{}
	published.Credentials = []desiredstate.Credential{}
	published.Inbounds = []desiredstate.Inbound{{
		ID: "inbound-1", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: false,
		CredentialIDs: []string{}, DefaultOutbound: "direct",
	}}
	for name, intent := range map[string][]string{"unknown intent": nil, "empty intent": {}} {
		next := mergeUserAccess(published, fresh, intent)
		if next.Inbounds[0].Enabled {
			t.Fatalf("%s re-enabled a disabled inbound: %+v", name, next.Inbounds[0])
		}
		if len(next.Inbounds[0].CredentialIDs) != 1 || next.Inbounds[0].CredentialIDs[0] != "cred-1" {
			t.Fatalf("%s did not bind the credential: %+v", name, next.Inbounds[0].CredentialIDs)
		}
		if err := next.Validate(); err != nil {
			t.Fatalf("%s does not validate: %v", name, err)
		}
	}
}

// TestMergeUserAccessDoesNotMutatePublished guards the copy rule for the shared
// WireGuard settings: the published state has to stay byte-identical, because it
// is the baseline every later automatic revision builds on.
func TestMergeUserAccessDoesNotMutatePublished(t *testing.T) {
	published, full := mergedWireGuardFixture()
	fresh := full
	fresh.Users = []desiredstate.User{{ID: "user-1", Enabled: true}}
	fresh.Credentials = []desiredstate.Credential{published.Credentials[0], published.Credentials[2]}
	inbound := published.Inbounds[0]
	inbound.CredentialIDs = []string{"wg-1", "relay"}
	settings := *inbound.Settings.WireGuard
	settings.Peers = peersWithout(published.Inbounds[0].Settings.WireGuard.Peers, "wg-2")
	inbound.Settings.WireGuard = &settings
	fresh.Inbounds = []desiredstate.Inbound{inbound}

	before, err := json.Marshal(published)
	if err != nil {
		t.Fatal(err)
	}
	next := mergeUserAccess(published, fresh, nil)
	after, err := json.Marshal(published)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("the merge mutated the published state:\nbefore %s\nafter  %s", before, after)
	}
	if next.Inbounds[0].Settings.WireGuard == published.Inbounds[0].Settings.WireGuard {
		t.Fatal("the merged inbound shares the published WireGuard settings")
	}
}

// TestMergeUserAccessNoOpEqualityWithReorderedFresh proves the ordering rule: a
// draft whose credential and binding order differs but whose user surface is the
// same merges back to the published bytes, so no spurious revision is created.
func TestMergeUserAccessNoOpEqualityWithReorderedFresh(t *testing.T) {
	published, fresh := mergedWireGuardFixture()
	fresh.Credentials = []desiredstate.Credential{published.Credentials[2], published.Credentials[1], published.Credentials[0]}
	inbound := published.Inbounds[0]
	inbound.CredentialIDs = []string{"relay", "wg-2", "wg-1"}
	settings := *inbound.Settings.WireGuard
	settings.Peers = []desiredstate.WireGuardPeer{
		published.Inbounds[0].Settings.WireGuard.Peers[3],
		published.Inbounds[0].Settings.WireGuard.Peers[2],
		published.Inbounds[0].Settings.WireGuard.Peers[1],
		published.Inbounds[0].Settings.WireGuard.Peers[0],
	}
	inbound.Settings.WireGuard = &settings
	fresh.Inbounds = []desiredstate.Inbound{inbound}

	next := mergeUserAccess(published, fresh, nil)
	if !quotaStatesEqual(published, next) {
		merged, _ := json.Marshal(next)
		original, _ := json.Marshal(published)
		t.Fatalf("a reordered draft must merge back to the published state:\npublished %s\nmerged    %s", original, merged)
	}
}

func stateOutboundIDs(state desiredstate.State) []string {
	ids := []string{}
	for _, outbound := range state.Outbounds {
		ids = append(ids, outbound.ID)
	}
	return ids
}

func statePolicyIDs(state desiredstate.State) []string {
	ids := []string{}
	for _, policy := range state.Policies {
		ids = append(ids, policy.ID)
	}
	return ids
}

// TestQuotaRevisionExcludesUnpublishedOwnerChanges is the core review fix: with
// an Owner-saved but unpublished outbound and policy in the database, a quota
// crossing still produces an automatic revision, but that revision only drops
// the user's credential. It must not carry the unpublished outbound or policy.
func TestQuotaRevisionExcludesUnpublishedOwnerChanges(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "unpublished_user", "")
	baseline := f.publish(t)

	// Owner saves an outbound and a policy but never publishes a revision.
	outboundResponse := f.owner.call("POST", "/api/v1/outbounds", proxy.OutboundInput{Name: "Draft exit", NodeID: f.nodeID, Protocol: "direct", Enabled: true, FailurePolicy: "Block"})
	if outboundResponse.Code != 201 {
		t.Fatalf("draft outbound: %d %s", outboundResponse.Code, outboundResponse.Body.String())
	}
	var draft proxy.Outbound
	if err := json.Unmarshal(outboundResponse.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	policyResponse := f.owner.call("POST", "/api/v1/policies", map[string]any{
		"name": "Draft block", "priority": 20, "enabled": true,
		"scope":  desiredstate.Scope{NodeIDs: []string{f.nodeID}},
		"match":  desiredstate.Match{Domains: []string{"draft.example"}},
		"action": desiredstate.Action{Type: "Block"},
	})
	if policyResponse.Code != 201 {
		t.Fatalf("draft policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}

	before := f.state(t, baseline)
	setQuota(t, f.ctx, f, account.ID, 50, 100)
	saveUser(t, f, account, 50)
	flowWait(t, "one automatic revision removing the credential", func() bool {
		return len(f.autoRevisions(t)) == 1
	})
	state := f.state(t, f.autoRevisions(t)[0].ID)

	if stateHasCredential(state, credential) {
		t.Fatal("over-quota credential survived in the automatic revision")
	}
	if strings.Join(stateOutboundIDs(state), ",") != strings.Join(stateOutboundIDs(before), ",") {
		t.Fatalf("outbound set changed: before=%v after=%v", stateOutboundIDs(before), stateOutboundIDs(state))
	}
	for _, id := range stateOutboundIDs(state) {
		if id == draft.ID {
			t.Fatal("unpublished outbound was published by the automatic revision")
		}
	}
	if strings.Join(statePolicyIDs(state), ",") != strings.Join(statePolicyIDs(before), ",") {
		t.Fatalf("policy set changed: before=%v after=%v", statePolicyIDs(before), statePolicyIDs(state))
	}
}

// TestQuotaReactivationRestoresCredentialToMatchingInboundOnly checks that a
// reactivated credential is restored to the inbound whose protocol matches it,
// and never to an inbound of another protocol on the same node.
func TestQuotaReactivationRestoresCredentialToMatchingInboundOnly(t *testing.T) {
	f := newQuotaFixture(t)
	// A second, different-protocol inbound that must never receive the VLESS
	// credential: it has no socks5 credential assigned, so it stays disabled.
	socksResponse := f.owner.call("POST", "/api/v1/inbounds", proxy.InboundInput{Name: "Quota SOCKS", NodeID: f.nodeID, Protocol: "socks5", Listen: "0.0.0.0", Port: 24445, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if socksResponse.Code != 201 {
		t.Fatalf("socks inbound: %d %s", socksResponse.Code, socksResponse.Body.String())
	}

	account, credential := f.addUser(t, "matching_user", "")
	f.publish(t)
	setQuota(t, f.ctx, f, account.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{account.ID})
	flowWait(t, "the over-quota removal", func() bool { return len(f.autoRevisions(t)) == 1 })
	f.apply(t, f.autoRevisions(t)[0].ID)

	if response := f.owner.call("POST", "/api/v1/users/"+account.ID+"/reset-traffic", nil); response.Code != 204 {
		t.Fatalf("reset traffic: %d %s", response.Code, response.Body.String())
	}
	flowWait(t, "one reactivation revision restoring the credential", func() bool { return len(f.autoRevisions(t)) == 2 })
	state := f.state(t, f.autoRevisions(t)[1].ID)

	vless, socks := inboundForTest(t, state, "vless"), inboundForTest(t, state, "socks5")
	if !vless.Enabled || len(vless.CredentialIDs) != 1 || vless.CredentialIDs[0] != credential.ID {
		t.Fatalf("matching inbound was not restored: %+v", vless)
	}
	if socks.Enabled || len(socks.CredentialIDs) != 0 {
		t.Fatalf("credential leaked to the mismatched inbound: %+v", socks)
	}
}

func inboundForTest(t *testing.T, state desiredstate.State, protocol string) desiredstate.Inbound {
	t.Helper()
	found := []desiredstate.Inbound{}
	for _, inbound := range state.Inbounds {
		if inbound.Protocol == protocol {
			found = append(found, inbound)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected one %s inbound, got %d", protocol, len(found))
	}
	return found[0]
}

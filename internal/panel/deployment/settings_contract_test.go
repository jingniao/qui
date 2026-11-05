package deployment

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func testRealitySettings(t *testing.T) *desiredstate.RealityServerSettings {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &desiredstate.RealityServerSettings{
		PrivateKey:      base64.RawURLEncoding.EncodeToString(key.Bytes()),
		PublicKey:       base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		HandshakeTarget: "www.example.com:443",
		ServerNames:     []string{"www.example.com"},
		ShortIDs:        []string{"0123abcd"},
	}
}

// The publish path repeats the capability gate: an Agent that has not declared the
// settings contract cannot receive xhttp/Reality settings, even from a revision
// that was created before its capability report arrived (a restore, or a draft
// written while the core was still being installed). The revision itself stays
// structurally valid — M1 stores typed settings and refuses them at the gates.
func TestDeployRefusesServerSettingsTheAgentCannotRender(t *testing.T) {
	r, _, _, rev := coreNodeFixture(t, "linux/amd64", "xray")
	ctx := context.Background()
	state, err := r.State(ctx, rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	state.Credentials = []desiredstate.Credential{{ID: "vless-credential", Protocol: "vless", Secret: "3f8b1a8a-1b2c-4d5e-8f90-1234567890ab"}}
	state.Inbounds = []desiredstate.Inbound{{
		ID: "reality-in", Protocol: "vless", Listen: "0.0.0.0", Port: 24443, Enabled: true, DefaultOutbound: "direct",
		CredentialIDs: []string{"vless-credential"},
		Settings:      desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: testRealitySettings(t)},
	}}
	draft, err := r.Create(ctx, state, "owner", "reality inbound")
	if err != nil {
		t.Fatalf("the typed settings were refused before the capability gate: %v", err)
	}
	if err := r.Deploy(ctx, draft.ID); err == nil || !strings.Contains(err.Error(), "settings contract") {
		t.Fatalf("a revision the Agent cannot render was published: %v", err)
	}
	// The baseline revision of the same node still deploys: the gate adds no
	// requirement to a state without the new settings.
	state.Inbounds = nil
	state.Credentials = nil
	plain, err := r.Create(ctx, state, "owner", "plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Deploy(ctx, plain.ID); err != nil {
		t.Fatalf("a legacy revision was refused: %v", err)
	}
}

// The revision comparison exposes the public half of a self-hosted listener and
// hides the Reality private key: a rotation is visible as a change, the key itself
// never is.
func TestRevisionDiffHidesTheRealityPrivateKey(t *testing.T) {
	previousReality := testRealitySettings(t)
	nextReality := testRealitySettings(t)
	nextReality.HandshakeTarget = "edge.example.com:443"
	previous := desiredstate.State{Revision: 1, Inbounds: []desiredstate.Inbound{{ID: "in", Protocol: "vless", Settings: desiredstate.ProtocolSettings{
		Security: desiredstate.SecurityReality, Reality: previousReality,
		Transport: desiredstate.TransportXHTTP, XHTTP: &desiredstate.XHTTPSettings{Host: "cdn.example.com", Path: "/a", Mode: desiredstate.XHTTPModeAuto},
	}}}}
	next := desiredstate.State{Revision: 2, Inbounds: []desiredstate.Inbound{{ID: "in", Protocol: "vless", Settings: desiredstate.ProtocolSettings{
		Security: desiredstate.SecurityReality, Reality: nextReality,
		Transport: desiredstate.TransportXHTTP, XHTTP: &desiredstate.XHTTPSettings{Host: "edge.example.com", Path: "/a", Mode: desiredstate.XHTTPModeAuto},
	}}}}
	changes := compareStates(previous, next)
	fields := map[string]Change{}
	for _, change := range changes {
		fields[change.Field] = change
	}
	key, ok := fields["settings.reality.private_key"]
	if !ok || !key.Hidden || key.Before != "[已隐藏]" || key.After != "[已隐藏]" {
		t.Fatalf("reality key change: %+v", key)
	}
	if host := fields["settings.xhttp.host"]; host.Before != `"cdn.example.com"` || host.After != `"edge.example.com"` {
		t.Fatalf("xhttp host change: %+v", host)
	}
	if target := fields["settings.reality.handshake_target"]; target.Hidden || target.Before != `"www.example.com:443"` || target.After != `"edge.example.com:443"` {
		t.Fatalf("reality handshake target change: %+v", target)
	}
	for _, change := range changes {
		for _, value := range []string{change.Before, change.After} {
			if strings.Contains(value, previousReality.PrivateKey) || strings.Contains(value, nextReality.PrivateKey) {
				t.Fatalf("a reality private key reached the comparison: %s", change.Field)
			}
		}
	}
	if again := compareStates(next, next); len(again) != 0 {
		t.Fatalf("an unchanged revision reported changes: %+v", again)
	}
}

// The publish gate opens for a node that reported the contract — here in the
// scalar shape a single-core Agent uses — so the report travels from storage to
// the deployed revision.
func TestDeployAcceptsServerSettingsTheAgentDeclared(t *testing.T) {
	r, _, nodeID, baseline := coreNodeFixture(t, "linux/amd64", "xray")
	ctx := context.Background()
	capabilities := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27",
		ServerSettingsContractVersion: desiredstate.SettingsContractVersion,
		ServerSettingsCombinations:    []*pb.SettingsCombination{{Protocol: "vless", Direction: desiredstate.DirectionInbound, Security: desiredstate.SecurityReality, Transport: "tcp"}}}
	raw, err := protojson.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), nodeID); err != nil {
		t.Fatal(err)
	}
	state, err := r.State(ctx, baseline.ID)
	if err != nil {
		t.Fatal(err)
	}
	state.Credentials = []desiredstate.Credential{{ID: "vless-credential", Protocol: "vless", Secret: "3f8b1a8a-1b2c-4d5e-8f90-1234567890ab"}}
	state.Inbounds = []desiredstate.Inbound{{
		ID: "reality-in", Protocol: "vless", Listen: "0.0.0.0", Port: 24443, Enabled: true, DefaultOutbound: "direct",
		CredentialIDs: []string{"vless-credential"},
		Settings:      desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: testRealitySettings(t)},
	}}
	draft, err := r.Create(ctx, state, "owner", "declared reality inbound")
	if err != nil {
		t.Fatalf("the typed settings were refused before the capability gate: %v", err)
	}
	if err := r.Deploy(ctx, draft.ID); err != nil {
		t.Fatalf("a revision the Agent declared was refused: %v", err)
	}
}

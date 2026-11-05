package proxy

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
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

// A node may advertise VLESS, a current core version and the multi-core shape and
// still be unable to apply xhttp/Reality: the panel refuses to store settings the
// node's core has not declared, so an older Agent is failed closed instead of
// receiving a configuration that silently drops them. Legacy settings keep saving
// exactly as before.
func TestSaveInboundRefusesServerSettingsWithoutTheAgentContract(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	record := func(nodeID string, caps *pb.Capabilities) {
		t.Helper()
		if _, err := f.nodes.Record(ctx, nodeID, &pb.AgentMessage{NodeId: nodeID, MessageId: "capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: caps}}); err != nil {
			t.Fatal(err)
		}
	}
	vless := []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true, Udp: true}}
	record(f.nodeA, &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", MultiCoreSupported: true, Cores: []*pb.CoreCapability{
		{Name: "xray", Version: "26.3.27", Protocols: vless},
		{Name: "sing-box", Version: "1.14.3", Protocols: vless},
	}})
	third, _, _, err := f.nodes.CreateEnrollment(ctx, "Node C", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	save := func(nodeID string, settings desiredstate.ProtocolSettings) (Inbound, error) {
		return f.service.SaveInbound(ctx, "", InboundInput{Name: "vless entry", NodeID: nodeID, Protocol: "vless", Listen: "0.0.0.0", Port: 24443, Enabled: true, DefaultOutbound: "direct", Settings: settings})
	}
	reality := desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: testRealitySettings(t)}
	if _, err := save(f.nodeA, reality); err == nil || !strings.Contains(err.Error(), "settings contract") {
		t.Fatalf("reality inbound accepted without a settings contract: %v", err)
	}
	xhttp := desiredstate.ProtocolSettings{Transport: desiredstate.TransportXHTTP, XHTTP: &desiredstate.XHTTPSettings{Host: "cdn.example.com", Path: "/assets", Mode: desiredstate.XHTTPModeAuto}}
	if _, err := save(f.nodeA, xhttp); err == nil || !strings.Contains(err.Error(), "settings contract") {
		t.Fatalf("xhttp inbound accepted without a settings contract: %v", err)
	}
	// A node that has not reported a core at all cannot apply them either.
	if _, err := save(third.ID, reality); err == nil || !strings.Contains(err.Error(), "not reported enabled") {
		t.Fatalf("unreported core accepted xhttp/Reality settings: %v", err)
	}
	// The legacy transport still saves on the same node.
	if _, err := save(f.nodeA, desiredstate.ProtocolSettings{Transport: "ws", Path: "/ws"}); err != nil {
		t.Fatalf("legacy websocket inbound refused: %v", err)
	}
	if err := ValidateInboundServerSettings(nil, "", "vless", reality); err == nil {
		t.Fatal("absent capabilities authorized xhttp/Reality settings")
	}
}

// The stored row is the other half of the contract: a Reality listener read back
// through the ordinary path must not disclose its private key, while the Owner's
// reveal path (used to render it to the Agent) still has it.
func TestStoredRealityInboundIsMaskedOnRead(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	id := insertRealityInbound(t, f, "reality-in")
	masked, err := f.service.Repository.Inbound(ctx, id, false)
	if err != nil {
		t.Fatal(err)
	}
	if masked.Settings.Reality == nil || masked.Settings.Reality.PrivateKey != "" || !masked.HasRealityKey {
		t.Fatalf("ordinary read exposed the reality key: %+v", masked.Settings.Reality)
	}
	if !masked.HasRealityKey || masked.Settings.Reality.PublicKey == "" {
		t.Fatal("the masked read lost the public half or the key flag")
	}
	revealed, err := f.service.Repository.Inbound(ctx, id, true)
	if err != nil || revealed.Settings.Reality == nil || revealed.Settings.Reality.PrivateKey == "" {
		t.Fatalf("the reveal path lost the stored key: %v", err)
	}
}

// The configuration snapshot repeats the gate: a stored listener that no core has
// declared cannot reach a node through the publish path.
func TestSnapshotRefusesServerSettingsTheCoreDidNotDeclare(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.nodes.Record(ctx, f.nodeA, &pb.AgentMessage{NodeId: f.nodeA, MessageId: "capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true, Udp: true}}}}}); err != nil {
		t.Fatal(err)
	}
	insertRealityInbound(t, f, "reality-in")
	if _, err := f.service.Snapshot(ctx, f.nodeA, f.users); err == nil || !strings.Contains(err.Error(), "settings contract") {
		t.Fatalf("the snapshot carried settings no core declared: %v", err)
	}
}

// insertRealityInbound writes one stored Reality listener the way an archive from
// a future version would: through the same sealed settings column, without the
// save gate (which refuses it in M1).
func insertRealityInbound(t *testing.T, f *fixture, name string) string {
	t.Helper()
	settings := desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: testRealitySettings(t)}
	sealed, err := f.service.Repository.encode(settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Repository.Database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at,core) VALUES(?,?,?,'vless','0.0.0.0',24444,0,0,0,'direct','[]',?,'2026-11-05T00:00:00Z','xray')", name, name, f.nodeA, sealed); err != nil {
		t.Fatal(err)
	}
	return name
}

// The in-memory half of the same rule: masking clears only the private key, and an
// edit that echoes the masked record keeps the stored key. Rotation is a
// deliberate new value, not a side effect of saving.
func TestRealityPrivateKeyIsMaskedAndPreserved(t *testing.T) {
	settings := desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: testRealitySettings(t)}
	stored, public := settings.Reality.PrivateKey, settings.Reality.PublicKey
	masked := mask(settings)
	if masked.Reality == nil || masked.Reality.PrivateKey != "" {
		t.Fatal("the reality private key survived redaction")
	}
	if masked.Reality.PublicKey != public || masked.Reality.HandshakeTarget != settings.Reality.HandshakeTarget || len(masked.Reality.ShortIDs) != 1 {
		t.Fatal("masking dropped public reality values")
	}
	if settings.Reality.PrivateKey != stored {
		t.Fatal("masking mutated the stored settings")
	}
	merged := mergeSettings(masked, settings)
	if merged.Reality == nil || merged.Reality.PrivateKey != stored {
		t.Fatal("the stored reality key was lost by a masked edit")
	}
	rotated := desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: testRealitySettings(t)}
	if merged = mergeSettings(rotated, settings); merged.Reality.PrivateKey != rotated.Reality.PrivateKey {
		t.Fatal("a supplied reality key was overwritten")
	}
	if legacy := mergeSettings(desiredstate.ProtocolSettings{Transport: "ws", Path: "/ws"}, desiredstate.ProtocolSettings{}); legacy.Reality != nil {
		t.Fatal("the merge invented reality settings")
	}
}

// A report that declares a combination opens the same gates: the panel stores the
// settings and the snapshot carries them to the node. A neighbouring combination
// the core did not declare stays refused.
func TestSaveInboundAcceptsServerSettingsTheCoreDeclared(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	vless := []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true, Udp: true}}
	capabilities := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", MultiCoreSupported: true, Cores: []*pb.CoreCapability{
		{Name: "xray", Version: "26.3.27", Protocols: vless, ServerSettingsContractVersion: desiredstate.SettingsContractVersion,
			ServerSettingsCombinations: []*pb.SettingsCombination{
				{Protocol: "vless", Direction: desiredstate.DirectionInbound, Security: desiredstate.SecurityReality, Transport: "tcp"},
				{Protocol: "vless", Direction: desiredstate.DirectionInbound, Transport: desiredstate.TransportXHTTP},
			}},
	}}
	if _, err := f.nodes.Record(ctx, f.nodeA, &pb.AgentMessage{NodeId: f.nodeA, MessageId: "capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: capabilities}}); err != nil {
		t.Fatal(err)
	}
	save := func(port int, settings desiredstate.ProtocolSettings) (Inbound, error) {
		return f.service.SaveInbound(ctx, "", InboundInput{Name: "vless entry", NodeID: f.nodeA, Protocol: "vless", Listen: "0.0.0.0", Port: port, Enabled: true, DefaultOutbound: "direct", Settings: settings})
	}
	reality := desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: testRealitySettings(t)}
	if _, err := save(24443, reality); err != nil {
		t.Fatalf("the declared reality combination was refused at save: %v", err)
	}
	xhttp := desiredstate.ProtocolSettings{Transport: desiredstate.TransportXHTTP, XHTTP: &desiredstate.XHTTPSettings{Host: "cdn.example.com", Path: "/assets", Mode: desiredstate.XHTTPModeAuto}}
	if _, err := save(24444, xhttp); err != nil {
		t.Fatalf("the declared xhttp combination was refused at save: %v", err)
	}
	vision := reality
	vision.Flow = "xtls-rprx-vision"
	if _, err := save(24445, vision); err == nil || !strings.Contains(err.Error(), "settings combination") {
		t.Fatalf("an undeclared combination was saved: %v", err)
	}
	if _, _, err := f.service.SnapshotIntent(ctx, f.nodeA, f.users); err != nil {
		t.Fatalf("the snapshot refused settings the core declared: %v", err)
	}
}

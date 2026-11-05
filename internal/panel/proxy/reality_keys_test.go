package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestRealityKeyCreationOrdinaryEditRotationAndEncryption(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	caps := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true}}, ServerSettingsContractVersion: 1, ServerSettingsCombinations: []*pb.SettingsCombination{{Protocol: "vless", Direction: "inbound", Security: "reality", Transport: "tcp"}}}
	if _, err := f.nodes.Record(ctx, f.nodeA, &pb.AgentMessage{NodeId: f.nodeA, MessageId: "key-capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: caps}}); err != nil {
		t.Fatal(err)
	}
	input := InboundInput{Name: "reality", NodeID: f.nodeA, Protocol: "vless", Listen: "127.0.0.1", Port: 24443, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Transport: "tcp", Security: "reality", Reality: &desiredstate.RealityServerSettings{HandshakeTarget: "127.0.0.1:443", ServerNames: []string{"panel.invalid"}, ShortIDs: []string{"01"}}}}
	created, err := f.service.SaveInbound(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.service.Repository.Inbound(ctx, created.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := stored.Settings.Reality.PrivateKey
	if oldKey == "" || stored.Settings.Reality.PublicKey == "" {
		t.Fatal("creation did not generate a key pair")
	}
	public, err := f.service.Repository.Inbound(ctx, created.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), oldKey) || !public.HasRealityKey {
		t.Fatal("ordinary query exposed private key or lost key flag")
	}
	var sealed []byte
	if err := f.service.Repository.Database.QueryRowContext(ctx, "SELECT encrypted_settings FROM inbounds WHERE id=?", created.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), oldKey) {
		t.Fatal("private key stored in plaintext")
	}
	input.Settings = public.Settings
	input.Name = "ordinary edit"
	if _, err := f.service.SaveInbound(ctx, created.ID, input); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := f.service.Repository.Inbound(ctx, created.ID, true)
	if unchanged.Settings.Reality.PrivateKey != oldKey {
		t.Fatal("ordinary edit rotated key")
	}
	input.RotateRealityKey = true
	if _, err := f.service.SaveInbound(ctx, created.ID, input); err != nil {
		t.Fatal(err)
	}
	rotated, _ := f.service.Repository.Inbound(ctx, created.ID, true)
	if rotated.Settings.Reality.PrivateKey == oldKey || rotated.Settings.Reality.PublicKey == stored.Settings.Reality.PublicKey {
		t.Fatal("explicit rotation retained the old key")
	}
	if stored.Settings.Reality.PrivateKey != oldKey {
		t.Fatal("rotation mutated the older snapshot")
	}
	input.RotateRealityKey = false
	input.Settings = desiredstate.ProtocolSettings{Transport: "tcp", Security: "none"}
	if _, err := f.service.SaveInbound(ctx, created.ID, input); err != nil {
		t.Fatal(err)
	}
	input.Settings = desiredstate.ProtocolSettings{Transport: "tcp", Security: "reality", Reality: &desiredstate.RealityServerSettings{HandshakeTarget: "127.0.0.1:443", ServerNames: []string{"panel.invalid"}, ShortIDs: []string{"01"}}}
	if _, err := f.service.SaveInbound(ctx, created.ID, input); err != nil {
		t.Fatal("existing listener conversion did not generate new Reality keys", err)
	}
	converted, _ := f.service.Repository.Inbound(ctx, created.ID, true)
	if converted.Settings.Reality.PrivateKey == "" || converted.Settings.Reality.PrivateKey == rotated.Settings.Reality.PrivateKey {
		t.Fatal("conversion reused removed private material")
	}
	input.InternalRelay = true
	if _, err := f.service.SaveInbound(ctx, created.ID, input); err == nil {
		t.Fatal("inbound-only settings became an internal relay")
	}
}

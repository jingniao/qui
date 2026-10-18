package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
	"time"
)

type fixture struct {
	service      *Service
	users        *user.Repository
	nodes        *node.Repository
	nodeA, nodeB string
	databasePath string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	directory := t.TempDir()
	database, err := db.Open(context.Background(), filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	configurationVault, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	first, _, _, err := nodes.CreateEnrollment(context.Background(), "Node A", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := nodes.CreateEnrollment(context.Background(), "Node B", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	nodes.SetPublicIP(context.Background(), first.ID, "192.0.2.10")
	nodes.SetPublicIP(context.Background(), second.ID, "192.0.2.11")
	return &fixture{service: &Service{Repository: &Repository{Database: database, Vault: configurationVault}, Nodes: nodes}, users: &user.Repository{Database: database, Vault: configurationVault}, nodes: nodes, nodeA: first.ID, nodeB: second.ID}
}
func TestRelayCredentialsAreIndependentEncryptedAndHidden(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	target, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "TLS relay", NodeID: f.nodeB, Protocol: "trojan", Listen: "0.0.0.0", Port: 8443, Enabled: true, InternalRelay: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if target.SubscriptionVisible || !target.HasTLSKey || target.Settings.PrivateKeyPEM != "" {
		t.Fatal("relay exposure or TLS secret visible")
	}
	first, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "Relay one", NodeID: f.nodeA, Protocol: "trojan", Enabled: true, TargetInboundID: target.ID, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "Relay two", NodeID: f.nodeA, Protocol: "trojan", Enabled: true, TargetInboundID: target.ID, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.CredentialID == second.CredentialID {
		t.Fatal("relay links reused credential")
	}
	one, err := f.service.Repository.Credential(ctx, first.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	two, err := f.service.Repository.Credential(ctx, second.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	if one.Secret == two.Secret || !one.Internal || one.UserID != "" {
		t.Fatal("invalid internal credential separation")
	}
	var stored []byte
	if err := f.service.Repository.Database.QueryRow("SELECT encrypted_secret FROM relay_credentials WHERE id=?", one.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(one.Secret)) {
		t.Fatal("relay secret unencrypted")
	}
	targetState, err := f.service.Snapshot(ctx, f.nodeB, f.users)
	if err != nil {
		t.Fatal(err)
	}
	if len(targetState.Inbounds) != 1 || len(targetState.Inbounds[0].CredentialIDs) != 3 || targetState.Inbounds[0].SubscriptionVisible {
		t.Fatal("target relay bindings missing")
	}
	sourceState, err := f.service.Snapshot(ctx, f.nodeA, f.users)
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceState.Outbounds) != 4 {
		t.Fatal("source outbounds missing")
	}
	encoded, _ := json.Marshal(first)
	if bytes.Contains(encoded, []byte(one.Secret)) || bytes.Contains(encoded, []byte("PRIVATE KEY")) {
		t.Fatal("outbound DTO leaked secret")
	}
	if err := f.service.Repository.DeleteInbound(ctx, target.ID); err == nil {
		t.Fatal("deleted referenced relay inbound")
	}
	backTarget, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "A relay", NodeID: f.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "B to A loop", NodeID: f.nodeB, Protocol: "socks5", Enabled: true, TargetInboundID: backTarget.ID, SupportTCP: true, SupportUDP: true}); err == nil {
		t.Fatal("relay cycle accepted")
	}
}
func TestPortConflictAndWireGuardEditPreservesIdentity(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	input := InboundInput{Name: "WG relay", NodeID: f.nodeB, Protocol: "wireguard", Listen: "0.0.0.0", Port: 51820, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"}
	target, err := f.service.SaveInbound(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveInbound(ctx, "", input); err == nil {
		t.Fatal("duplicate node port accepted")
	}
	outInput := OutboundInput{Name: "WG source", NodeID: f.nodeA, Protocol: "wireguard", Enabled: true, TargetInboundID: target.ID, SupportTCP: true, SupportUDP: true}
	out, err := f.service.SaveOutbound(ctx, "", outInput)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.service.Repository.Outbound(ctx, out.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveOutbound(ctx, out.ID, outInput); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.Repository.Outbound(ctx, out.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if before.Settings.WireGuard.SecretKey != after.Settings.WireGuard.SecretKey || before.Settings.WireGuard.Addresses[0] != after.Settings.WireGuard.Addresses[0] {
		t.Fatal("editing WireGuard replaced assigned identity")
	}
	snapshot, err := f.service.Snapshot(ctx, f.nodeB, f.users)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Inbounds[0].Settings.WireGuard.Peers) != 1 {
		t.Fatal("WireGuard relay peer not generated")
	}
	if err := desiredstate.ValidateSettings("wireguard", snapshot.Inbounds[0].Settings, true); err != nil {
		t.Fatal(err)
	}
}

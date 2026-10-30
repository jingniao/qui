package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"testing"
)

func TestUserWireGuardSnapshotOnlyContainsPeerPublicKey(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	group, err := f.nodes.SaveGroup(ctx, "", node.GroupInput{Name: "User WG", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{f.nodeA}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := f.users.Save(ctx, "", user.Input{Username: "wg_user", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"wireguard"}, NodeGroupIDs: []string{group.ID}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := f.users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "WG device", Protocols: []string{"wireguard"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "WG user entry", NodeID: f.nodeA, Protocol: "wireguard", Listen: "0.0.0.0", Port: 51820, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: group.Purpose, Enabled: true, SubscriptionVisible: true, NodeIDs: []string{f.nodeA}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.service.Snapshot(ctx, f.nodeA, f.users)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(snapshot)
	if bytes.Contains(encoded, []byte(device.Credentials[0].Secret)) {
		t.Fatal("device WireGuard private key sent to node")
	}
	if len(snapshot.Credentials) != 1 || snapshot.Credentials[0].PublicKey != device.Credentials[0].PublicKey || snapshot.Credentials[0].Secret != "" {
		t.Fatal("public peer identity missing")
	}
	if len(snapshot.Inbounds[0].Settings.WireGuard.Peers) != 1 {
		t.Fatal("user peer missing")
	}
	firstAddress := snapshot.Inbounds[0].Settings.WireGuard.Peers[0].AllowedIPs[0]
	if _, err := f.users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Second WG device", Protocols: []string{"wireguard"}}); err != nil {
		t.Fatal(err)
	}
	updated, err := f.service.Snapshot(ctx, f.nodeA, f.users)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, peer := range updated.Inbounds[0].Settings.WireGuard.Peers {
		if peer.CredentialID == device.Credentials[0].ID {
			found = true
			if peer.AllowedIPs[0] != firstAddress {
				t.Fatal("new device reassigned existing WireGuard address")
			}
		}
	}
	if !found {
		t.Fatal("existing device peer disappeared")
	}
}

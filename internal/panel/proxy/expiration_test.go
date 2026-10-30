package proxy

import (
	"context"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"testing"
	"time"
)

func TestProxySnapshotIncludesExpiryAndBusinessEditDoesNotChangeOldSnapshot(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	group, err := f.nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Expiry entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{f.nodeA}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	input := user.Input{Username: "expires_user", Status: "active", ExpiresAt: deadline, MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"socks5"}, NodeGroupIDs: []string{group.ID}}
	u, err := f.users.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	deadline = u.ExpiresAt
	if _, err := f.users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Device", Protocols: []string{"socks5"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "User entry", NodeID: f.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, DefaultOutbound: "direct", SubscriptionVisible: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: group.Purpose, Enabled: true, SubscriptionVisible: true, NodeIDs: []string{f.nodeA}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.service.Snapshot(ctx, f.nodeA, f.users)
	if err != nil || len(snapshot.Users) != 1 || snapshot.Users[0].ExpiresAt != deadline {
		t.Fatal("expiry missing from approved node input", snapshot.Users, err)
	}
	input.ExpiresAt = time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	updated, err := f.users.Save(ctx, u.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Users[0].ExpiresAt != deadline {
		t.Fatal("draft expiry altered prior snapshot")
	}
	next, err := f.service.Snapshot(ctx, f.nodeA, f.users)
	if err != nil || next.Users[0].ExpiresAt != updated.ExpiresAt {
		t.Fatal("new snapshot did not capture new deadline")
	}
}

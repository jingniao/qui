package deployment

import (
	"context"
	"encoding/json"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	"strings"
	"testing"
	"time"
)

type stubUserAccess struct {
	access []user.Access
}

func (s stubUserAccess) AccessForGroups(context.Context, []string) ([]user.Access, error) {
	return s.access, nil
}

func TestUserAccessStateCarriesNodeOutboundNetworkOnlyWhenConfigured(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "User entry", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{Nodes: nodes, Users: stubUserAccess{access: []user.Access{{User: user.User{ID: "user-1", Status: "active"}, Credentials: []user.Credential{{ID: "credential-1", UserID: "user-1", DeviceID: "device-1", Protocol: "socks5", Username: "device", Secret: "private-device-password"}}}}}}
	input := UserAccessInput{NodeID: n.ID, Summary: "User access", Protocol: "socks5", Listen: "127.0.0.1", Port: 1080}
	plain, err := handler.UserState(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Network != nil {
		t.Fatal("a node without settings drafted a network block")
	}
	data, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "network") {
		t.Fatalf("unconfigured node serialized a network block: %s", data)
	}
	sourceIP, iface, ipv4, ipv6 := "192.0.2.10", "eth0", "198.51.100.4", "2001:db8::4"
	if err := nodes.UpdateSettings(ctx, n.ID, node.SettingsInput{Name: "User entry", Enabled: true, OutboundSourceIP: &sourceIP, OutboundInterface: &iface, HostSourceIPv4: &ipv4, HostSourceIPv6: &ipv6}); err != nil {
		t.Fatal(err)
	}
	configured, err := handler.UserState(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if configured.Network == nil || configured.Network.OutboundSourceIP != sourceIP || configured.Network.OutboundInterface != iface || configured.Network.HostSourceIPv4 != ipv4 || configured.Network.HostSourceIPv6 != ipv6 {
		t.Fatalf("user access draft lost the node settings: %+v", configured.Network)
	}
	if err := configured.Validate(); err != nil {
		t.Fatalf("user access draft with settings does not validate: %v", err)
	}
}

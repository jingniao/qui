package subscription

import (
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// previewAddressService publishes one VLESS inbound on a node whose addresses
// are given, so the preview can be asked which address a client would dial.
func previewAddressService(publicIP, connectHost string) *Service {
	account := user.User{ID: previewUserID, Status: "active", AllowedProtocols: []string{"vless"}, NodeGroupIDs: []string{"preview-group"}}
	device := user.Device{ID: previewDeviceID, UserID: account.ID, Status: "active"}
	credentials := []user.Credential{{ID: "vless-credential", UserID: account.ID, DeviceID: device.ID, Protocol: "vless", Secret: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", Status: "active"}}
	users := previewUsers{account: account, device: device, credentials: credentials}
	n := node.Node{ID: "preview-node", Name: "Preview", Status: "online", Enabled: true, AllowSubscription: true, PublicIP: publicIP, ConnectHost: connectHost, Capabilities: &pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Tcp: true}}}}
	state := desiredstate.State{
		SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true},
		Credentials: []desiredstate.Credential{{ID: "vless-credential", UserID: account.ID, DeviceID: device.ID, Protocol: "vless", Secret: credentials[0].Secret}},
		Inbounds:    []desiredstate.Inbound{{ID: "vless-in", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: true, SubscriptionVisible: true, CredentialIDs: []string{"vless-credential"}, DefaultOutbound: "direct"}},
		Outbounds:   []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
	return &Service{
		Users:   users,
		Sources: source.NewRegistry(&published.Source{Users: users, Nodes: previewNodes{n}, Published: previewPublished{n.ID: state}}),
		Formats: DefaultFormats(),
	}
}

// The connect host is what a client dials, so it wins over the detected public
// address the nodes use among themselves.
func TestPreviewPositionsTheConnectHostOverThePublicAddress(t *testing.T) {
	body, _ := readPreview(t, &Handler{Service: previewAddressService("203.0.113.7", "edge.example.com")}, previewUserID, previewDeviceID)
	if len(body.Items) != 1 || body.Items[0].Address != "edge.example.com" || len(body.Warnings) != 0 {
		t.Fatalf("preview items %+v warnings %+v", body.Items, body.Warnings)
	}
	if body.Items[0].Link == nil || !strings.Contains(*body.Items[0].Link, "@edge.example.com:443") {
		t.Fatalf("preview link %v", body.Items[0].Link)
	}
	body, _ = readPreview(t, &Handler{Service: previewAddressService("203.0.113.7", "")}, previewUserID, previewDeviceID)
	if len(body.Items) != 1 || body.Items[0].Address != "203.0.113.7" {
		t.Fatalf("preview without a connect host: %+v", body.Items)
	}
}

// A node with neither address cannot produce an endpoint, and the Owner is told
// which node it is instead of receiving a silently short subscription.
func TestPreviewWarnsWhenANodeHasNoConnectAddress(t *testing.T) {
	body, _ := readPreview(t, &Handler{Service: previewAddressService("", "")}, previewUserID, previewDeviceID)
	if body.Total != 0 || len(body.Items) != 0 {
		t.Fatalf("endpoint without an address: %+v", body.Items)
	}
	if len(body.Warnings) != 1 || body.Warnings[0].Stage != "source" || body.Warnings[0].Ref != "published" {
		t.Fatalf("preview warnings %+v", body.Warnings)
	}
	if want := "节点「Preview」未设置连接地址，订阅不包含它的入站"; body.Warnings[0].Message != want {
		t.Fatalf("warning %q, want %q", body.Warnings[0].Message, want)
	}
}

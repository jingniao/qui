package deployment

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
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

// userAccessFixture is the smallest panel the quick user-access feature needs:
// an enrolled node and the proxy service that owns the node's inbounds.
type userAccessFixture struct {
	handler *Handler
	proxies *proxy.Repository
	nodes   *node.Repository
	nodeID  string
	entries []user.Access
}

func newUserAccessFixture(t *testing.T) *userAccessFixture {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	configurationVault, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "User entry", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	proxies := &proxy.Repository{Database: database, Vault: configurationVault}
	f := &userAccessFixture{handler: &Handler{Nodes: nodes, Proxy: &proxy.Service{Repository: proxies, Nodes: nodes}}, proxies: proxies, nodes: nodes, nodeID: n.ID}
	f.entries = []user.Access{{User: user.User{ID: "user-1", Status: "active"}, Credentials: []user.Credential{{ID: "credential-1", UserID: "user-1", DeviceID: "device-1", Protocol: "socks5", Username: "device", Secret: "private-device-password"}}}}
	f.handler.Users = stubUserAccess{access: f.entries}
	return f
}

func (f *userAccessFixture) addUser(id string) {
	f.entries = append(f.entries, user.Access{User: user.User{ID: id, Status: "active"}, Credentials: []user.Credential{{ID: "credential-" + id, UserID: id, DeviceID: "device-" + id, Protocol: "socks5", Username: "name-" + id, Secret: "private-password-" + id}}})
	f.handler.Users = stubUserAccess{access: f.entries}
}

// stateInbound returns the rendered inbound for a protocol.
func stateInbound(t *testing.T, state desiredstate.State, protocol string) desiredstate.Inbound {
	t.Helper()
	for _, inbound := range state.Inbounds {
		if inbound.Protocol == protocol {
			return inbound
		}
	}
	t.Fatalf("the rendered state has no %s inbound", protocol)
	return desiredstate.Inbound{}
}

// TestUserAccessFeatureWritesOneInboundBoundToItsUsers is the fix for the
// quick user-access draft: its inbound used to exist only inside the revision,
// so the proxy renderer and every automatic quota revision could not see it and
// stripped the user's credential. It is now business data that a second publish
// reuses instead of colliding with.
func TestUserAccessFeatureWritesOneInboundBoundToItsUsers(t *testing.T) {
	f := newUserAccessFixture(t)
	ctx := context.Background()
	input := UserAccessInput{NodeID: f.nodeID, Summary: "User access", Protocol: "socks5", Listen: "0.0.0.0", Port: 1080}
	plain, enabled, err := f.handler.EnsureUserAccess(ctx, input)
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
	inbound, err := f.proxies.InboundAtListener(ctx, f.nodeID, "socks5", "0.0.0.0", 1080)
	if err != nil {
		t.Fatalf("the user access draft did not create an inbound row: %v", err)
	}
	if !inbound.Enabled || !inbound.SubscriptionVisible || inbound.InternalRelay || inbound.DefaultOutbound != "direct" {
		t.Fatalf("user access inbound: %+v", inbound)
	}
	if len(inbound.AllowedUserIDs) != 1 || inbound.AllowedUserIDs[0] != "user-1" {
		t.Fatalf("user access inbound bindings: %v", inbound.AllowedUserIDs)
	}
	rendered := stateInbound(t, plain, "socks5")
	if rendered.ID != inbound.ID || !rendered.Enabled || len(rendered.CredentialIDs) != 1 || rendered.CredentialIDs[0] != "credential-1" {
		t.Fatalf("rendered user access inbound: %+v", rendered)
	}
	// The intent names the inbound the feature's business data enabled, which is
	// what a later automatic revision may re-enable.
	if len(enabled) != 1 || enabled[0] != inbound.ID {
		t.Fatalf("the renderer reported %v as enabled, want the user access inbound %s", enabled, inbound.ID)
	}
	if len(plain.Credentials) != 1 || plain.Credentials[0].ID != "credential-1" || plain.Credentials[0].Internal {
		t.Fatalf("rendered user access credentials: %+v", plain.Credentials)
	}

	// A second draft on the same listener reuses the row and adds the users that
	// appeared meanwhile, instead of failing on the port it already occupies.
	f.addUser("user-2")
	second, _, err := f.handler.EnsureUserAccess(ctx, input)
	if err != nil {
		t.Fatalf("reusing the listener failed: %v", err)
	}
	reused, err := f.proxies.InboundAtListener(ctx, f.nodeID, "socks5", "0.0.0.0", 1080)
	if err != nil {
		t.Fatal(err)
	}
	if reused.ID != inbound.ID {
		t.Fatalf("the second draft created another inbound: %s then %s", inbound.ID, reused.ID)
	}
	if len(reused.AllowedUserIDs) != 2 {
		t.Fatalf("reused bindings: %v", reused.AllowedUserIDs)
	}
	if ids := stateInbound(t, second, "socks5").CredentialIDs; len(ids) != 2 {
		t.Fatalf("reused inbound credentials: %v", ids)
	}

	sourceIP, iface, ipv4, ipv6 := "192.0.2.10", "eth0", "198.51.100.4", "2001:db8::4"
	if err := f.nodes.UpdateSettings(ctx, f.nodeID, node.SettingsInput{Name: "User entry", Enabled: true, OutboundSourceIP: &sourceIP, OutboundInterface: &iface, HostSourceIPv4: &ipv4, HostSourceIPv6: &ipv6}); err != nil {
		t.Fatal(err)
	}
	configured, _, err := f.handler.EnsureUserAccess(ctx, input)
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

// TestRawRevisionRejectsRevisionOnlyUserCredentials: a raw revision cannot
// carry a user credential, because no business data backs it and the next
// render would silently drop it. The Owner has to publish the credential
// through the users/inbounds they own.
func TestRawRevisionRejectsRevisionOnlyUserCredentials(t *testing.T) {
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "node", Logging: desiredstate.Logging{DomainOnly: true},
		Credentials: []desiredstate.Credential{{ID: "credential", Protocol: "http", Username: "manual-account", Secret: "manual-password"}},
		Inbounds:    []desiredstate.Inbound{{ID: "entry", Protocol: "http", Listen: "127.0.0.1", Port: 1080, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"credential"}}},
		Outbounds:   []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	body, err := json.Marshal(map[string]any{"summary": "Manual entry", "state": state})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/v1/revisions", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	// The rejection happens before the repository or the actor are touched, so
	// the handler can be exercised without a session.
	(&Handler{}).create(response, request)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "unmanaged_user_credentials") {
		t.Fatalf("raw revision with a user credential: %d %s", response.Code, response.Body.String())
	}
}

package subscription

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
	"net/url"
	"path/filepath"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil/dbtest"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	database    *sql.DB
	users       *user.Repository
	nodes       *node.Repository
	deployments *deployment.Repository
	service     *Service
	u           user.User
	device      user.Device
	nodeIDs     []string
	groups      []node.Group
}

func setup(t *testing.T) *fixture {
	return setupWithLimit(t, 0)
}
func setupWithLimit(t *testing.T, limit int64) *fixture {
	return setupProtocols(t, limit, "socks5", "http")
}

// setupProtocols is the fixture of an account whose only protocols are the given
// ones, so a case can use a protocol the default fixture does not carry.
func setupProtocols(t *testing.T, limit int64, protocols ...string) *fixture {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	database, err := dbtest.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	configurationVault, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{database: database, users: &user.Repository{Database: database, Vault: configurationVault}, nodes: &node.Repository{Database: database}, deployments: &deployment.Repository{Database: database, Vault: configurationVault}}
	f.service = &Service{
		Repository: &Repository{Database: database},
		Users:      f.users,
		Sources:    source.NewRegistry(&published.Source{Users: f.users, Nodes: f.nodes, Published: f.deployments}),
		Formats:    DefaultFormats(),
	}
	capabilities := []*pb.ProtocolCapability{}
	for _, protocol := range protocols {
		capabilities = append(capabilities, &pb.ProtocolCapability{Protocol: protocol, Inbound: true, Tcp: true})
	}
	for index := 0; index < 3; index++ {
		n, _, _, err := f.nodes.CreateEnrollment(ctx, "Node "+string(rune('A'+index)), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		f.nodeIDs = append(f.nodeIDs, n.ID)
		if _, err := f.nodes.Record(ctx, n.ID, &pb.AgentMessage{Sequence: 1, Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", Protocols: capabilities}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.nodes.Record(ctx, n.ID, &pb.AgentMessage{Sequence: 2, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{PublicIp: "192.0.2." + string(rune('1'+index))}}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, nodeID := range f.nodeIDs {
		for index, protocol := range protocols {
			sealed, e := configurationVault.Seal([]byte("{}"))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES(?,?,?,?,'0.0.0.0',?,1,1,0,'direct','[]',?,'now')", nodeID+"-in-"+protocol, protocol, nodeID, protocol, 1080+index, sealed); e != nil {
				t.Fatal(e)
			}
		}
	}
	for index, members := range [][]string{{f.nodeIDs[0]}, {f.nodeIDs[0], f.nodeIDs[1]}, {f.nodeIDs[2]}} {
		purpose := "subscription"
		if index == 2 {
			purpose = "relay"
		}
		group, err := f.nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Group " + string(rune('A'+index)), Purpose: purpose, Enabled: true, SubscriptionVisible: true, NodeIDs: members})
		if err != nil {
			t.Fatal(err)
		}
		f.groups = append(f.groups, group)
	}
	input := user.Input{Username: "sub_user", Status: "active", TrafficLimit: limit, MaxDevices: 3, MaxConnections: 8, AllowedProtocols: protocols, NodeGroupIDs: []string{f.groups[0].ID, f.groups[1].ID, f.groups[2].ID}}
	f.u, err = f.users.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	f.device, err = f.users.CreateDevice(ctx, f.u.ID, user.DeviceInput{Name: "Laptop", Protocols: protocols})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *fixture) publish(t *testing.T, nodeID string) {
	t.Helper()
	inbounds := []desiredstate.Inbound{}
	for index, credential := range f.device.Credentials {
		inbounds = append(inbounds, inbound(nodeID+"-in-"+credential.Protocol, credential.Protocol, 1080+index, credential.ID))
	}
	f.publishInbounds(t, nodeID, inbounds...)
}

// publishInbounds applies one successful revision of nodeID whose inbounds are
// the given ones, so a case can publish endpoints the fixture account does not
// create on its own.
func (f *fixture) publishInbounds(t *testing.T, nodeID string, inbounds ...desiredstate.Inbound) {
	t.Helper()
	ctx := context.Background()
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: nodeID, Users: []desiredstate.User{{ID: f.u.ID, Enabled: true, ExpiresAt: f.u.ExpiresAt}}, Credentials: []desiredstate.Credential{}, Inbounds: inbounds, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}, Logging: desiredstate.Logging{DomainOnly: true}}
	for _, credential := range f.device.Credentials {
		state.Credentials = append(state.Credentials, desiredstate.Credential{ID: credential.ID, UserID: f.u.ID, DeviceID: f.device.ID, Protocol: credential.Protocol, Username: credential.Username, Secret: credential.Secret})
	}
	revision, err := f.deployments.Create(ctx, state, "owner", "Subscription integration")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deployments.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.deployments.Result(ctx, nodeID, &pb.ApplyResult{Revision: revision.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
}

// inbound describes one subscription-visible inbound of a node bound to the
// account's credential of its protocol.
func inbound(id, protocol string, port int, credentialID string) desiredstate.Inbound {
	return desiredstate.Inbound{ID: id, Protocol: protocol, Listen: "0.0.0.0", Port: port, Enabled: true, SubscriptionVisible: true, CredentialIDs: []string{credentialID}, DefaultOutbound: "direct"}
}

// credentialID is the account's credential ID of one protocol.
func (f *fixture) credentialID(t *testing.T, protocol string) string {
	t.Helper()
	for _, credential := range f.device.Credentials {
		if credential.Protocol == protocol {
			return credential.ID
		}
	}
	t.Fatalf("the fixture account has no %s credential", protocol)
	return ""
}

// addInbound registers another inbound on a node, the way the Owner creates one.
// Access still comes only from the node group.
func (f *fixture) addInbound(t *testing.T, nodeID, protocol string, port int) string {
	t.Helper()
	id := nodeID + "-in-" + protocol + "-" + strconv.Itoa(port)
	sealed, err := f.users.Vault.Seal([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES(?,?,?,?,'0.0.0.0',?,1,1,0,'direct','[]',?,'now')", id, id, nodeID, protocol, port, sealed); err != nil {
		t.Fatal(err)
	}
	return id
}

// grantInbounds grants exactly the given inbounds from the first node's group,
// the way the Owner grants access from the group page.
func (f *fixture) grantInbounds(t *testing.T, inbounds ...string) {
	t.Helper()
	group := f.groups[0]
	if _, err := f.nodes.SaveGroup(context.Background(), group.ID, node.GroupInput{Name: group.Name, Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: inbounds}); err != nil {
		t.Fatal(err)
	}
}
func TestGroupExpansionPublishedCredentialsAndFormats(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, nodeID := range f.nodeIDs {
		f.publish(t, nodeID)
	}
	endpoints, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID)
	if err != nil || len(endpoints) != 4 {
		t.Fatalf("deduplicated two nodes/two protocols expected: %d %v", len(endpoints), err)
	}
	for _, endpoint := range endpoints {
		if endpoint.Origin.Node.ID == f.nodeIDs[2] {
			t.Fatal("relay entered subscription")
		}
	}
	token, plaintext, err := f.service.Rotate(ctx, f.u.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := f.database.QueryRow("SELECT token_hash FROM subscription_tokens WHERE id=?", token.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == plaintext || strings.Contains(hash, plaintext) {
		t.Fatal("raw token stored")
	}
	list, err := f.service.Render(ctx, plaintext, "uri", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := f.service.Render(ctx, plaintext, "base64", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	original, err := base64.StdEncoding.DecodeString(string(decoded.Body))
	if err != nil || !bytes.Equal(original, list.Body) {
		t.Fatal("base64 export differs from URI")
	}
	for _, line := range strings.Fields(string(list.Body)) {
		parsed, err := url.Parse(line)
		if err != nil || parsed.User == nil || parsed.Host == "" {
			t.Fatal("invalid exported URI", err)
		}
	}
	sing, err := f.service.Render(ctx, plaintext, "sing-box", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ Outbounds []map[string]any }
	if err := json.Unmarshal(sing.Body, &document); err != nil || len(document.Outbounds) != 4 {
		t.Fatal("invalid sing-box export", err)
	}
	_, newToken, err := f.service.Rotate(ctx, f.u.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Render(ctx, plaintext, "uri", "", nil); err != ErrToken {
		t.Fatal("old token remained usable")
	}
	if _, err := f.service.Render(ctx, newToken, "uri", "", nil); err != nil {
		t.Fatal(err)
	}
	group := f.groups[0]
	_, err = f.nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{f.nodeIDs[0], f.nodeIDs[2]}})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, _, err = f.service.Proxies(ctx, f.u.ID, f.device.ID)
	if err != nil || len(endpoints) != 6 {
		t.Fatalf("new group member did not expand existing grant: %d %v", len(endpoints), err)
	}
}
func TestSubscriptionFiltersDisabledMaintenanceQuotaAndRevocation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, nodeID := range f.nodeIDs {
		f.publish(t, nodeID)
	}
	if err := f.nodes.SubscriptionFlags(ctx, f.nodeIDs[0], true, true); err != nil {
		t.Fatal(err)
	}
	if err := f.nodes.SubscriptionFlags(ctx, f.nodeIDs[1], false, false); err != nil {
		t.Fatal(err)
	}
	endpoints, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID)
	if err != nil || len(endpoints) != 0 {
		t.Fatal("maintenance or hidden node exported", err)
	}
	f.nodes.SubscriptionFlags(ctx, f.nodeIDs[0], true, false)
	if _, err := f.users.RotateCredential(ctx, f.device.ID, "socks5", true); err != nil {
		t.Fatal(err)
	}
	endpoints, _, err = f.service.Proxies(ctx, f.u.ID, f.device.ID)
	if err != nil || len(endpoints) != 1 || endpoints[0].Protocol != "http" {
		t.Fatal("revoked protocol still exported", err)
	}
	if _, err := f.users.RotateCredential(ctx, f.device.ID, "http", false); err != nil {
		t.Fatal(err)
	}
	endpoints, _, err = f.service.Proxies(ctx, f.u.ID, f.device.ID)
	if err != nil || len(endpoints) != 0 {
		t.Fatal("unpublished new credential exported", err)
	}
	f.users.UpdateDevice(ctx, f.device.ID, user.DeviceInput{Name: "Laptop", Status: "disabled"})
	if _, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID); err != user.ErrCredential {
		t.Fatal("disabled device exported", err)
	}
}

// A user with a traffic limit is served while under the limit; once used
// traffic reaches the limit, user.Active() reports inactive and the
// subscription is refused. Quota enforcement lives entirely in user.Active().
func TestSubscriptionTrafficLimitAllowsBelowAndRefusesAtLimit(t *testing.T) {
	f := setupWithLimit(t, 100)
	ctx := context.Background()
	f.publish(t, f.nodeIDs[0])
	endpoints, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID)
	if err != nil || len(endpoints) != 2 {
		t.Fatalf("user below traffic limit got no endpoints: %d %v", len(endpoints), err)
	}
	if _, err := f.database.Exec("UPDATE users SET used_traffic=? WHERE id=?", int64(100), f.u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID); err != user.ErrInactive {
		t.Fatalf("user at traffic limit not refused: %v", err)
	}
}

// Non-online nodes stay in the subscription: a pending or offline node has no
// published inbound yet, so the inbound step drops it rather than the node
// filter. Only disabled, maintenance and non-subscribable nodes are excluded.
func TestSubscriptionIncludesNonOnlineNodesAndExcludesDisabled(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, nodeID := range f.nodeIDs[:2] {
		f.publish(t, nodeID)
	}
	if endpoints, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID); err != nil || len(endpoints) != 4 {
		t.Fatalf("baseline endpoints: %d %v", len(endpoints), err)
	}
	if _, err := f.database.Exec("UPDATE nodes SET status='offline' WHERE id=?", f.nodeIDs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.Exec("UPDATE nodes SET status='degraded' WHERE id=?", f.nodeIDs[1]); err != nil {
		t.Fatal(err)
	}
	if endpoints, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID); err != nil || len(endpoints) != 4 {
		t.Fatalf("offline or degraded node dropped: %d %v", len(endpoints), err)
	}
	if _, err := f.database.Exec("UPDATE nodes SET allow_subscription=0 WHERE id=?", f.nodeIDs[1]); err != nil {
		t.Fatal(err)
	}
	if endpoints, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID); err != nil || len(endpoints) != 2 {
		t.Fatalf("non-subscribable node exported: %d %v", len(endpoints), err)
	}
	if _, err := f.database.Exec("UPDATE nodes SET enabled=0,status='disabled' WHERE id=?", f.nodeIDs[0]); err != nil {
		t.Fatal(err)
	}
	if endpoints, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID); err != nil || len(endpoints) != 0 {
		t.Fatalf("disabled node exported: %d %v", len(endpoints), err)
	}
	if _, err := f.database.Exec("UPDATE nodes SET enabled=1,status='offline',maintenance=1 WHERE id=?", f.nodeIDs[0]); err != nil {
		t.Fatal(err)
	}
	if endpoints, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID); err != nil || len(endpoints) != 0 {
		t.Fatalf("maintenance node exported: %d %v", len(endpoints), err)
	}
}

// F8: one node with two inbounds of the same protocol publishes both. A client
// identifies a server by its name, so a name that occurs once stays
// "<node> · <protocol>" and every endpoint of a colliding group names its port.
func TestTwoInboundsOfOneProtocolStayDistinct(t *testing.T) {
	for _, protocol := range []string{"socks5", "vless"} {
		t.Run(protocol, func(t *testing.T) {
			f := setupProtocols(t, 0, protocol)
			ctx := context.Background()
			credentialID := f.credentialID(t, protocol)
			first := f.nodeIDs[0] + "-in-" + protocol
			second := f.addInbound(t, f.nodeIDs[0], protocol, 2080)
			f.grantInbounds(t, first, second)
			one := inbound(first, protocol, 1080, credentialID)
			two := inbound(second, protocol, 2080, credentialID)
			f.publishInbounds(t, f.nodeIDs[0], one, two)

			names := endpointNames(f.endpoints(t))
			want := []string{"Node A · " + protocol + " · 1080", "Node A · " + protocol + " · 2080"}
			if strings.Join(names, " | ") != strings.Join(want, " | ") {
				t.Fatalf("endpoint names %v, want %v", names, want)
			}

			// Both endpoints reach the exported formats, one link each.
			rendered, err := f.service.Preview(ctx, f.u.ID, f.device.ID, "uri")
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(rendered.Body)), "\n")
			ports := []string{}
			for _, line := range lines {
				parsed, err := url.Parse(line)
				if err != nil {
					t.Fatalf("invalid URI %q: %v", line, err)
				}
				ports = append(ports, parsed.Port())
			}
			if strings.Join(ports, ",") != "1080,2080" {
				t.Fatalf("URI ports %v", ports)
			}
			profile, err := f.service.Preview(ctx, f.u.ID, f.device.ID, "mihomo")
			if err != nil {
				t.Fatal(err)
			}
			var document mihomoDocument
			if err := yaml.Unmarshal(profile.Body, &document); err != nil {
				t.Fatal(err)
			}
			proxies := []string{}
			for _, endpoint := range document.Proxies {
				proxies = append(proxies, endpoint.Name)
			}
			if strings.Join(proxies, " | ") != strings.Join(want, " | ") {
				t.Fatalf("mihomo proxies %v, want %v", proxies, want)
			}
			// A group the user picks a server in offers both endpoints.
			pickable := false
			for _, group := range document.Groups {
				if strings.Join(group.Members, " | ") == strings.Join(want, " | ") {
					pickable = true
				}
			}
			if !pickable {
				t.Fatalf("no mihomo group offers both endpoints: %+v", document.Groups)
			}

			// The order in which the node published its inbounds is not an input.
			f.publishInbounds(t, f.nodeIDs[0], two, one)
			reversed := endpointNames(f.endpoints(t))
			if strings.Join(reversed, " | ") != want[1]+" | "+want[0] {
				t.Fatalf("endpoint names after the reversed publish %v", reversed)
			}
		})
	}
}

// F8: only an endpoint this account actually gets takes part in naming, so a
// hidden, disabled or ungranted second inbound leaves the first name alone.
func TestOneEndpointKeepsItsPlainName(t *testing.T) {
	f := setupProtocols(t, 0, "socks5")
	credentialID := f.credentialID(t, "socks5")
	first := f.nodeIDs[0] + "-in-socks5"
	second := f.addInbound(t, f.nodeIDs[0], "socks5", 2080)
	for _, testCase := range []struct {
		name   string
		grant  bool
		change func(*desiredstate.Inbound)
	}{
		{name: "hidden", grant: true, change: func(endpoint *desiredstate.Inbound) { endpoint.SubscriptionVisible = false }},
		{name: "disabled", grant: true, change: func(endpoint *desiredstate.Inbound) { endpoint.Enabled = false }},
		{name: "ungranted", change: func(*desiredstate.Inbound) {}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			granted := []string{first}
			if testCase.grant {
				granted = append(granted, second)
			}
			f.grantInbounds(t, granted...)
			invisible := inbound(second, "socks5", 2080, credentialID)
			testCase.change(&invisible)
			f.publishInbounds(t, f.nodeIDs[0], inbound(first, "socks5", 1080, credentialID), invisible)
			if names := endpointNames(f.endpoints(t)); strings.Join(names, " | ") != "Node A · socks5" {
				t.Fatalf("endpoint names %v", names)
			}
		})
	}
}

// endpoints is the account's rendered list, which fails the test on an error or a
// collision warning: a node's own endpoints must never collide.
func (f *fixture) endpoints(t *testing.T) []proxy.Proxy {
	t.Helper()
	endpoints, warnings, err := f.service.Proxies(context.Background(), f.u.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings %+v", warnings)
	}
	return endpoints
}

func endpointNames(endpoints []proxy.Proxy) []string {
	names := []string{}
	for _, endpoint := range endpoints {
		names = append(names, endpoint.Name)
	}
	return names
}

// mihomoDocument is the part of a Clash profile a subscription test reads.
type mihomoDocument struct {
	Proxies []struct {
		Name string `yaml:"name"`
	} `yaml:"proxies"`
	Groups []struct {
		Name    string   `yaml:"name"`
		Members []string `yaml:"proxies"`
	} `yaml:"proxy-groups"`
}

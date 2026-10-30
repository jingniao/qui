package subscription

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/url"
	"path/filepath"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
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
	f := &fixture{database: database, users: &user.Repository{Database: database, Vault: configurationVault}, nodes: &node.Repository{Database: database}, deployments: &deployment.Repository{Database: database, Vault: configurationVault}}
	f.service = &Service{
		Repository: &Repository{Database: database},
		Users:      f.users,
		Sources:    source.NewRegistry(&published.Source{Users: f.users, Nodes: f.nodes, Published: f.deployments}),
		Formats:    DefaultFormats(),
	}
	for index := 0; index < 3; index++ {
		n, _, _, err := f.nodes.CreateEnrollment(ctx, "Node "+string(rune('A'+index)), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		f.nodeIDs = append(f.nodeIDs, n.ID)
		if _, err := f.nodes.Record(ctx, n.ID, &pb.AgentMessage{Sequence: 1, Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "socks5", Inbound: true, Tcp: true}, {Protocol: "http", Inbound: true, Tcp: true}}}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.nodes.Record(ctx, n.ID, &pb.AgentMessage{Sequence: 2, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{PublicIp: "192.0.2." + string(rune('1'+index))}}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, nodeID := range f.nodeIDs {
		for index, protocol := range []string{"socks5", "http"} {
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
	input := user.Input{Username: "sub_user", Status: "active", TrafficLimit: limit, MaxDevices: 3, MaxConnections: 8, AllowedProtocols: []string{"socks5", "http"}, NodeGroupIDs: []string{f.groups[0].ID, f.groups[1].ID, f.groups[2].ID}}
	f.u, err = f.users.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	f.device, err = f.users.CreateDevice(ctx, f.u.ID, user.DeviceInput{Name: "Laptop", Protocols: []string{"socks5", "http"}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *fixture) publish(t *testing.T, nodeID string) {
	t.Helper()
	ctx := context.Background()
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: nodeID, Users: []desiredstate.User{{ID: f.u.ID, Enabled: true, ExpiresAt: f.u.ExpiresAt}}, Credentials: []desiredstate.Credential{}, Inbounds: []desiredstate.Inbound{}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}, Logging: desiredstate.Logging{DomainOnly: true}}
	for index, credential := range f.device.Credentials {
		state.Credentials = append(state.Credentials, desiredstate.Credential{ID: credential.ID, UserID: f.u.ID, DeviceID: f.device.ID, Protocol: credential.Protocol, Username: credential.Username, Secret: credential.Secret})
		state.Inbounds = append(state.Inbounds, desiredstate.Inbound{ID: nodeID + "-in-" + credential.Protocol, Protocol: credential.Protocol, Listen: "0.0.0.0", Port: 1080 + index, Enabled: true, SubscriptionVisible: true, CredentialIDs: []string{credential.ID}, DefaultOutbound: "direct"})
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

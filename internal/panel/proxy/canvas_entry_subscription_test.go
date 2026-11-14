package proxy_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil/dbtest"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestCanvasEntrySubscriptionRequiresSuccessfulPublication(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := dbtest.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	ids := []string{}
	for _, name := range []string{"A", "B"} {
		n, _, _, err := nodes.CreateEnrollment(ctx, name, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := nodes.SetPublicIP(ctx, n.ID, "192.0.2.10"); err != nil {
			t.Fatal(err)
		}
		capabilities, err := json.Marshal(&pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{
			{Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true},
			{Protocol: "trojan", Inbound: true, Outbound: true, Tcp: true, Udp: true},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=? WHERE id=?", string(capabilities), n.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "entry users", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: database, Vault: key}
	account, err := users.Save(ctx, "", user.Input{Username: "canvas-user", Status: "active", NodeGroupIDs: []string{group.ID}})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := users.AccessIdentity(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	revisions := &deployment.Repository{Database: database, Vault: key}
	service := &proxy.Service{Repository: &proxy.Repository{Database: database, Vault: key}, Nodes: nodes, EntryGroups: nodes, Deploy: &deployment.CanvasPublisher{Repository: revisions}}
	entryPort, relayPort := 20000, 8443
	scheme, err := service.SaveCanvasScheme(ctx, "", proxy.CanvasSchemeInput{Name: "subscription route", Execution: "server", Layout: proxy.CanvasLayout{
		Instances: []proxy.CanvasInstance{
			{InstanceID: "a", Ref: "server:" + ids[0], Role: "entry", EntryBinding: &proxy.CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: &entryPort, GroupMode: "explicit", GroupIDs: []string{group.ID}}},
			{InstanceID: "b", Ref: "server:" + ids[1], Role: "exit"},
		},
		Connections: []proxy.CanvasConnection{{ID: "a-b", Source: "a", Target: "b", Type: "tunnel", TunnelProtocol: "tls", TunnelPort: &relayPort, SupportTCP: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.PublishCanvasScheme(ctx, scheme.ID, "Owner", users)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := service.Repository.CanvasEntryBinding(ctx, scheme.ID, "a")
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := service.Repository.Inbound(ctx, binding.InboundID, false)
	if err != nil || inbound.Name != "A → B" || inbound.InternalRelay || !inbound.SubscriptionVisible {
		t.Fatalf("ordinary entry: %+v, %v", inbound, err)
	}
	subscriptions := &published.Source{Users: users, Nodes: nodes, Published: revisions}
	viewer := source.Viewer{UserID: account.ID, DeviceID: identity.ID, AllowedProtocols: account.AllowedProtocols, NodeGroupIDs: account.NodeGroupIDs}
	assertHidden := func() {
		t.Helper()
		endpoints, warnings, err := subscriptions.Proxies(ctx, viewer)
		if err != nil || len(warnings) != 0 || len(endpoints) != 0 {
			t.Fatalf("unconfirmed entry visible: %+v, %+v, %v", endpoints, warnings, err)
		}
	}
	assertHidden()
	for _, item := range result.Nodes {
		if item.NodeID == ids[0] {
			assertHidden()
		}
		if err := revisions.Result(ctx, item.NodeID, &pb.ApplyResult{Revision: item.Revision, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	endpoints, warnings, err := subscriptions.Proxies(ctx, viewer)
	if err != nil || len(warnings) != 0 || len(endpoints) != 1 {
		t.Fatalf("published entry: %+v, %+v, %v", endpoints, warnings, err)
	}
	endpoint := endpoints[0]
	t.Logf("published entry %q is exported as %q", inbound.Name, endpoint.Name)
	if endpoint.Origin.Ref != inbound.ID || endpoint.Origin.Node.ID != ids[0] || endpoint.Protocol != "socks5" || endpoint.Port != entryPort || endpoint.Server != "192.0.2.10" {
		t.Fatalf("wrong entry endpoint: %+v", endpoint)
	}
	credentials, err := users.Credentials(ctx, identity.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, credential := range credentials {
		if credential.Protocol == "socks5" && credential.Username == endpoint.Credential.Username && credential.Secret == endpoint.Credential.Password && credential.Secret != "" {
			matched = true
		}
	}
	if !matched {
		t.Fatal("subscription does not use the authorized user's credential")
	}
	outsider, err := users.Save(ctx, "", user.Input{Username: "canvas-outsider", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	otherIdentity, err := users.AccessIdentity(ctx, outsider.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherEndpoints, _, err := subscriptions.Proxies(ctx, source.Viewer{UserID: outsider.ID, DeviceID: otherIdentity.ID, AllowedProtocols: outsider.AllowedProtocols, NodeGroupIDs: account.NodeGroupIDs})
	if err != nil || len(otherEndpoints) != 0 {
		t.Fatalf("entry leaked to ungranted user: %+v, %v", otherEndpoints, err)
	}
}

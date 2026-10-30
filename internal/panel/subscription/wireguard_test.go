package subscription

import (
	"bytes"
	"context"
	"encoding/json"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestWireGuardSubscriptionUsesDevicePrivateKeyAndServerPublicPeer(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, err := f.users.Save(ctx, f.u.ID, user.Input{Username: f.u.Username, Status: "active", MaxDevices: 3, MaxConnections: 8, AllowedProtocols: []string{"http", "socks5", "wireguard"}, NodeGroupIDs: f.u.NodeGroupIDs})
	if err != nil {
		t.Fatal(err)
	}
	client, err := f.users.RotateCredential(ctx, f.device.ID, "wireguard", false)
	if err != nil {
		t.Fatal(err)
	}
	server, err := user.GenerateCredential("wireguard")
	if err != nil {
		t.Fatal(err)
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: f.nodeIDs[0], Users: []desiredstate.User{{ID: f.u.ID, Enabled: true}}, Credentials: []desiredstate.Credential{{ID: client.ID, UserID: f.u.ID, DeviceID: f.device.ID, Protocol: "wireguard", PublicKey: client.PublicKey}}, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}, Inbounds: []desiredstate.Inbound{{ID: "wg-in", Protocol: "wireguard", Listen: "0.0.0.0", Port: 51820, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", CredentialIDs: []string{client.ID}, Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: server.Value, Addresses: []string{"10.99.0.1/24"}, Peers: []desiredstate.WireGuardPeer{{PublicKey: client.PublicKey, AllowedIPs: []string{"10.99.0.2/32"}, CredentialID: client.ID}}, MTU: 1420}}}}}
	sealed, e := f.users.Vault.Seal([]byte("{}"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('wg-in','WG',?,'wireguard','0.0.0.0',51820,1,1,0,'direct','[]',?,'now')", f.nodeIDs[0], sealed); e != nil {
		t.Fatal(e)
	}
	if _, e = f.database.Exec("INSERT INTO node_group_inbounds VALUES(?,'wg-in')", f.groups[0].ID); e != nil {
		t.Fatal(e)
	}
	// The node has to advertise the WireGuard inbound capability before the
	// revision is published: publishing validates each inbound against the core
	// it is assigned to, and a WireGuard inbound is not part of the baseline
	// socks5/http report of the fixture.
	if _, err := f.nodes.Record(ctx, f.nodeIDs[0], &pb.AgentMessage{Sequence: 3, Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "wireguard", Inbound: true, Outbound: true, Tcp: true, Udp: true}}}}}); err != nil {
		t.Fatal(err)
	}
	revision, err := f.deployments.Create(ctx, state, "owner", "WG subscription")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deployments.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.deployments.Result(ctx, f.nodeIDs[0], &pb.ApplyResult{Revision: revision.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	endpoints, _, err := f.service.Proxies(ctx, f.u.ID, f.device.ID)
	if err != nil || len(endpoints) != 1 {
		t.Fatal("WG endpoint missing", err)
	}
	// The neutral model carries the client's own private key (a WireGuard client
	// config needs it) but never the server's, and the admin preview has no
	// credential field at all.
	encoded, err := json.Marshal(endpoints)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(server.Value)) {
		t.Fatal("subscription model leaked the server private key")
	}
	if endpoints[0].WireGuard.ServerPublicKey != server.PublicKey || endpoints[0].WireGuard.Addresses[0] != "10.99.0.2/32" {
		t.Fatal("wrong peer configuration")
	}
	content, err := singbox.Exporter{}.Render(format.Profile{Proxies: endpoints})
	if err != nil {
		t.Fatal(err)
	}
	var exported struct {
		Endpoints []struct {
			PrivateKey string `json:"private_key"`
			Peers      []struct {
				PublicKey string `json:"public_key"`
			} `json:"peers"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(content.Body, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.Endpoints) != 1 || exported.Endpoints[0].PrivateKey != client.Secret || exported.Endpoints[0].Peers[0].PublicKey != server.PublicKey {
		t.Fatal("WG client export used wrong identity")
	}
	if bytes.Contains(content.Body, []byte(server.Value)) {
		t.Fatal("WG export leaked server private key")
	}
}

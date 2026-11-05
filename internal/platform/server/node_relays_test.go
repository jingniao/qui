package server

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	subproxy "proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source/published"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestNodeRelayRealCredentialShareAndPortal(t *testing.T) {
	f := newQuotaFixture(t)
	a, ca := f.addUser(t, "relay_a", "")
	b, cb := f.addUser(t, "relay_b", "")
	f.publish(t)
	nodes := &node.Repository{Database: f.app.Database}
	entry, _, _, err := nodes.CreateEnrollment(f.ctx, "HK", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for id, address := range map[string]string{entry.ID: "203.0.113.1", f.nodeID: "203.0.113.2"} {
		if err = nodes.SetPublicIP(f.ctx, id, address); err != nil {
			t.Fatal(err)
		}
	}
	caps, _ := json.Marshal(&pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true}}})
	if _, err = f.app.Database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(caps), f.nodeID); err != nil {
		t.Fatal(err)
	}
	port := 32000
	response := f.owner.call("POST", "/api/v1/tunnels", proxy.TunnelInput{Name: "HK to US", Transport: "tls", Hops: []proxy.TunnelHop{{Role: "entry", NodeID: entry.ID, PortMode: "manual"}, {Role: "exit", NodeID: f.nodeID, PortMode: "manual", ListenPort: &port}}})
	var tunnel proxy.Tunnel
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &tunnel) != nil {
		t.Fatal(response.Code, response.Body.String())
	}
	inbound := f.publishedInbound(t, "vless")
	port = 32001
	response = f.owner.call("POST", "/api/v1/nodes/"+f.nodeID+"/relays", proxy.NodeRelayInput{TunnelID: tunnel.ID, InboundID: inbound.ID, PortMode: "manual", ListenPort: &port, SubscriptionVisible: true})
	var relay proxy.NodeRelay
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &relay) != nil || relay.ShareLink == nil {
		t.Fatal(response.Code, response.Body.String())
	}
	first := ca.Secret
	if b.ID < a.ID {
		first = cb.Secret
	}
	link, err := url.Parse(*relay.ShareLink)
	if err != nil || link.User.Username() != first || link.Host != "203.0.113.1:32001" {
		t.Fatal(relay.ShareLink, err)
	}
	firstID, otherSecret := a.ID, cb.Secret
	if b.ID < a.ID {
		firstID, otherSecret = b.ID, ca.Secret
	}
	if _, err = f.app.Database.Exec("DELETE FROM user_node_group_grants WHERE user_id=?", firstID); err != nil {
		t.Fatal(err)
	}
	response = f.owner.call("GET", "/api/v1/nodes/"+f.nodeID+"/relays", nil)
	var listed struct {
		Items []proxy.NodeRelay `json:"items"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &listed) != nil || len(listed.Items) != 1 || listed.Items[0].ShareLink == nil {
		t.Fatal(response.Code, response.Body.String())
	}
	link, err = url.Parse(*listed.Items[0].ShareLink)
	if err != nil || link.User.Username() != otherSecret {
		t.Fatal("share link used an unauthorized credential")
	}
	if _, err = f.app.Database.Exec("INSERT INTO user_node_group_grants(user_id,group_id) VALUES(?,?)", firstID, f.groupID); err != nil {
		t.Fatal(err)
	}
	// A Viewer must get null even though an Owner can obtain a real credential.
	response = f.owner.call("POST", "/api/v1/admins", map[string]string{"username": "relay_viewer", "displayName": "Viewer", "role": "Viewer", "password": "relay-viewer-password-123"})
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	viewer := budgetHTTP{app: f.app}
	response = viewer.call("POST", "/api/v1/auth/login", map[string]string{"username": "relay_viewer", "password": "relay-viewer-password-123"})
	var session auth.Session
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &session) != nil {
		t.Fatal(response.Code, response.Body.String())
	}
	viewer.cookie = response.Result().Cookies()[0]
	response = viewer.call("GET", "/api/v1/nodes/"+f.nodeID+"/relays", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"shareLink":null`) || strings.Contains(response.Body.String(), first) {
		t.Fatal(response.Body.String())
	}
	if response = f.owner.call("PUT", "/api/v1/users/"+a.ID+"/login", map[string]any{"enabled": true, "password": portalPassword}); response.Code != 204 {
		t.Fatal(response.Body.String())
	}
	portal := portalLogin(t, f.app, a.Username, portalPassword)
	response = portal.call("GET", "/api/v1/portal/nodes", nil)
	if response.Code != 200 || strings.Contains(response.Body.String(), "经 HK") {
		t.Fatal(response.Code, response.Body.String())
	}
	// Successful-publication persistence is tested in proxy; seed that receipt
	// here to exercise the real subscription database reader and portal route.
	snapshot, _ := json.Marshal(published.Relay{ID: relay.Rule.ID, InboundID: inbound.ID, Entry: subproxy.Ref{ID: entry.ID, Name: "HK"}, Server: "203.0.113.1", Port: 32001, DestinationAddress: "203.0.113.2", DestinationPort: inbound.Port, Network: "both", Enabled: true, Visible: true})
	if _, err = f.app.Database.Exec("UPDATE tunnel_rules SET published_snapshot=?,published_at=updated_at,published_version=draft_version,status='published' WHERE id=?", string(snapshot), relay.Rule.ID); err != nil {
		t.Fatal(err)
	}
	response = portal.call("GET", "/api/v1/portal/nodes", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Quota Node · vless · 经 HK") || strings.Contains(response.Body.String(), ca.Secret) {
		t.Fatal(response.Code, response.Body.String())
	}
	// Draft changes cannot remove or move the deployed subscription endpoint.
	if _, err = f.app.Database.Exec("UPDATE tunnel_rules SET subscription_visible=0,listen_port=32002,status='modified' WHERE id=?", relay.Rule.ID); err != nil {
		t.Fatal(err)
	}
	response = portal.call("GET", "/api/v1/portal/nodes", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "经 HK") {
		t.Fatal(response.Code, response.Body.String())
	}
}

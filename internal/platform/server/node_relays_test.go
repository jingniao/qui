package server

import (
	"encoding/json"
	"net/url"
	"slices"
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
	t.Parallel()
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
	if err != nil || link.User.Username() != first || link.Host != "203.0.113.1:32001" || link.Fragment != "Quota Node 经 HK to US" {
		t.Fatal(relay.ShareLink, err)
	}
	// The entry's client connect address replaces its public IP for clients.
	if _, err = f.app.Database.Exec("UPDATE nodes SET connect_host='hk.example' WHERE id=?", entry.ID); err != nil {
		t.Fatal(err)
	}
	response = f.owner.call("GET", "/api/v1/nodes/"+f.nodeID+"/relays", nil)
	var withHost struct {
		Items []proxy.NodeRelay `json:"items"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &withHost) != nil || len(withHost.Items) != 1 || withHost.Items[0].ShareLink == nil {
		t.Fatal(response.Code, response.Body.String())
	}
	if link, err = url.Parse(*withHost.Items[0].ShareLink); err != nil || link.Host != "hk.example:32001" {
		t.Fatal(*withHost.Items[0].ShareLink, err)
	}
	if _, err = f.app.Database.Exec("UPDATE nodes SET connect_host='' WHERE id=?", entry.ID); err != nil {
		t.Fatal(err)
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
	snapshot, _ := json.Marshal(published.Relay{ID: relay.Rule.ID, InboundID: inbound.ID, Entry: subproxy.Ref{ID: entry.ID, Name: "HK"}, Server: "203.0.113.1", Port: 32001, DestinationAddress: "203.0.113.2", DestinationPort: inbound.Port, Network: "both", Enabled: true, Visible: true, RouteNodeIDs: []string{entry.ID, f.nodeID}})
	if _, err = f.app.Database.Exec("UPDATE tunnel_rules SET published_snapshot=?,published_at=updated_at,published_version=draft_version,status='published' WHERE id=?", string(snapshot), relay.Rule.ID); err != nil {
		t.Fatal(err)
	}
	// A published rule reaches users only as a node group member of its own.
	response = portal.call("GET", "/api/v1/portal/nodes", nil)
	if response.Code != 200 || strings.Contains(response.Body.String(), "经 HK") {
		t.Fatal(response.Code, response.Body.String())
	}
	// Missing route proof on an ungranted rule must not affect this portal.
	// Once granted, its seeded successful snapshot needs the same index as a
	// real receipt; rebuild it through the production publication-proof reader.
	if err = f.app.tunnelPublications.Repository.RebuildSubscriptionDependencies(f.ctx); err != nil {
		t.Fatal(err)
	}
	group, err := f.app.nodeRepository.Group(f.ctx, f.groupID)
	if err != nil {
		t.Fatal(err)
	}
	saveGroup := func(inboundIDs, relayIDs []string) {
		t.Helper()
		body := map[string]any{"name": group.Name, "purpose": group.Purpose, "enabled": group.Enabled, "subscriptionVisible": group.SubscriptionVisible, "serverIds": group.ServerIDs, "inboundIds": inboundIDs, "imports": group.Imports, "relayIds": relayIDs, "remark": group.Remark}
		response := f.owner.call("PUT", "/api/v1/node-groups/"+f.groupID, body)
		var saved node.Group
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &saved) != nil || len(saved.RelayIDs) != len(relayIDs) {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	saveGroup(group.InboundIDs, []string{relay.Rule.ID})
	response = portal.call("GET", "/api/v1/portal/nodes", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Quota Node 经 HK to US") || strings.Contains(response.Body.String(), ca.Secret) {
		t.Fatal(response.Code, response.Body.String())
	}
	// The rule alone, without the target inbound, still reaches the user and
	// keeps their credential on the target server.
	saveGroup([]string{}, []string{relay.Rule.ID})
	var portalNodes struct {
		Items []struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Protocols []string `json:"protocols"`
		} `json:"items"`
	}
	response = portal.call("GET", "/api/v1/portal/nodes", nil)
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &portalNodes) != nil || len(portalNodes.Items) != 1 || portalNodes.Items[0].Name != "Quota Node 经 HK to US" {
		t.Fatal(response.Code, response.Body.String())
	}
	access, err := f.app.connectionObserver.Repository.AccessForGroups(f.ctx, []string{f.groupID}, nil)
	if err != nil || len(access) != 2 {
		t.Fatal(access, err)
	}
	for _, entry := range access {
		if !slices.Contains(entry.InboundIDs, inbound.ID) {
			t.Fatal("rule-only grant lost the target inbound credential", entry.InboundIDs)
		}
	}
	if groups, err := f.app.nodeRepository.SubscriptionGroups(f.ctx, f.nodeID, nil); err != nil || !slices.Contains(groups, f.groupID) {
		t.Fatal(groups, err)
	}
	saveGroup(group.InboundIDs, []string{relay.Rule.ID})
	// Draft changes cannot remove or move the deployed subscription endpoint.
	if _, err = f.app.Database.Exec("UPDATE tunnel_rules SET subscription_visible=0,listen_port=32002,status='modified' WHERE id=?", relay.Rule.ID); err != nil {
		t.Fatal(err)
	}
	response = portal.call("GET", "/api/v1/portal/nodes", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "经 HK") {
		t.Fatal(response.Code, response.Body.String())
	}
}

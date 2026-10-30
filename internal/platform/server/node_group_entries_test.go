package server

import (
	"encoding/json"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/portal"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestGroupInboundBoundarySubscriptionPortalAndQuota(t *testing.T) {
	f := newQuotaFixture(t)
	account, _ := f.addUser(t, "inbound_boundary", "")
	var entries []proxy.Inbound
	for _, port := range []int{24444, 24445} {
		r := f.owner.call("POST", "/api/v1/inbounds", proxy.InboundInput{Name: "extra", NodeID: f.nodeID, Protocol: "vless", Listen: "0.0.0.0", Port: port, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
		if r.Code != 201 {
			t.Fatal(r.Body.String())
		}
		var i proxy.Inbound
		if err := json.Unmarshal(r.Body.Bytes(), &i); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, i)
	}
	input := node.GroupInput{Name: "only selected", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: []string{entries[0].ID}}
	response := f.owner.call("PUT", "/api/v1/node-groups/"+f.groupID, input)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var g node.Group
	if err := json.Unmarshal(response.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.NodeIDs) != 1 || len(g.InboundIDs) != 1 || len(g.GrantedUserIDs) != 1 || g.GrantedUserIDs[0] != account.ID {
		t.Fatal(g)
	}
	if _, err := f.app.nodeRepository.Record(f.ctx, f.nodeID, &pb.AgentMessage{Sequence: 1, Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Tcp: true}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := f.app.nodeRepository.SetPublicIP(f.ctx, f.nodeID, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	f.publish(t)
	state, err := f.app.deployments.Published(f.ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range state.Inbounds {
		if (len(i.CredentialIDs) > 0) != (i.ID == entries[0].ID) {
			t.Fatal("credential crossed inbound boundary", i)
		}
	}
	// Use the real subscription service and real published credentials.
	p, _, err := (&published.Source{Users: f.app.quotaDeployer.users, Nodes: f.app.nodeRepository, Published: f.app.deployments}).Proxies(f.ctx, source.Viewer{UserID: account.ID, DeviceID: entriesDevice(t, f, account.ID), NodeGroupIDs: []string{f.groupID}, AllowedProtocols: []string{"vless"}})
	if err != nil || len(p) != 1 || p[0].Origin.Ref != entries[0].ID {
		t.Fatal(p, err)
	}
	portalService := &portal.Service{Users: f.app.quotaDeployer.users, Nodes: f.app.nodeRepository}
	nodes, err := portalService.GrantedNodes(f.ctx, account.ID)
	if err != nil || len(nodes) != 1 {
		t.Fatal(nodes, err)
	}
	// Remove every grant while the running node still holds its credential.
	if r := f.owner.call("DELETE", "/api/v1/users/"+account.ID+"/node-groups/"+f.groupID, nil); r.Code != 204 {
		t.Fatal(r.Code, r.Body.String())
	}
	flowWait(t, "revoked credential removal queued for its former server", func() bool { return len(f.autoRevisions(t)) == 1 })
	automatic := f.autoRevisions(t)[0]
	if stateHoldsUser(f.state(t, automatic.ID), account.ID) {
		t.Fatal("credential retained after revoke")
	}
	nodes, err = portalService.GrantedNodes(f.ctx, account.ID)
	if err != nil || len(nodes) != 0 {
		t.Fatal(nodes, err)
	}
}

func entriesDevice(t *testing.T, f *quotaFixture, userID string) string {
	t.Helper()
	d, _, err := f.app.quotaDeployer.users.Devices(f.ctx, userID, 1, 10)
	if err != nil || len(d) != 1 {
		t.Fatal(d, err)
	}
	return d[0].ID
}

func TestGroupEntriesAndPortRangeAPIDecode(t *testing.T) {
	f := newQuotaFixture(t)
	for _, body := range []any{
		map[string]any{"portRangeStart": 10000},
		map[string]any{"portRangeStart": 10002, "portRangeEnd": 10001},
		map[string]any{"portRangeStart": 0, "portRangeEnd": 65536},
		map[string]any{"portRangeStart": 1.5, "portRangeEnd": 10},
		map[string]any{"unknown": true},
	} {
		r := f.owner.call("PUT", "/api/v1/nodes/"+f.nodeID+"/meta", body)
		if r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r := f.owner.call("PUT", "/api/v1/nodes/"+f.nodeID+"/meta", map[string]any{"portRangeStart": 10000, "portRangeEnd": 10001, "renewNoticeDays": 7})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	m, _, err := f.app.nodeRepository.Meta(f.ctx, f.nodeID)
	if err != nil || m.PortRangeStart == nil || *m.PortRangeEnd != 10001 {
		t.Fatal(m, err)
	}
	for _, body := range []any{
		map[string]any{"name": "bad", "purpose": "subscription", "inboundIds": []string{"missing"}},
		map[string]any{"name": "bad", "purpose": "subscription", "imports": []map[string]string{{"sourceId": "missing", "proxyKey": ""}}},
		map[string]any{"name": "bad", "purpose": "subscription", "inboundIds": []string{}, "unknown": true},
	} {
		r := f.owner.call("POST", "/api/v1/node-groups", body)
		if r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	// Empty lists decode successfully and the returned server index is empty.
	r = f.owner.call("POST", "/api/v1/node-groups", map[string]any{"name": "empty entries", "purpose": "subscription", "enabled": true, "subscriptionVisible": true, "inboundIds": []string{}, "imports": []any{}})
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestExplicitGroupServerAPIAndAccessIsolation(t *testing.T) {
	f := newQuotaFixture(t)
	account, _ := f.addUser(t, "server_only", "")
	body := map[string]any{"name": "explicit only", "purpose": "subscription", "enabled": true, "subscriptionVisible": true, "serverIds": []string{f.nodeID}, "inboundIds": []string{}}
	response := f.owner.call("PUT", "/api/v1/node-groups/"+f.groupID, body)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var g node.Group
	if err := json.Unmarshal(response.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.ServerIDs) != 1 || g.ServerIDs[0] != f.nodeID || len(g.NodeIDs) != 1 || len(g.InboundIDs) != 0 {
		t.Fatal(g)
	}
	list := f.owner.call("GET", "/api/v1/node-groups", nil)
	var groups struct{ Items []node.Group }
	if err := json.Unmarshal(list.Body.Bytes(), &groups); err != nil || list.Code != 200 {
		t.Fatal(list.Code, list.Body.String(), err)
	}
	found := false
	for _, v := range groups.Items {
		if v.ID == g.ID && len(v.ServerIDs) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("GET omitted explicit servers", list.Body.String())
	}
	allowed, err := f.app.nodeRepository.UserHasNode(f.ctx, f.nodeID, account.ID)
	if err != nil || allowed {
		t.Fatal("explicit server granted access", allowed, err)
	}
	state, err := f.app.quotaDeployer.proxy.Snapshot(f.ctx, f.nodeID, f.app.quotaDeployer.users)
	if err != nil {
		t.Fatal(err)
	}
	if stateHoldsUser(state, account.ID) {
		t.Fatal("explicit server granted credentials")
	}
	proxies, _, err := (&published.Source{Users: f.app.quotaDeployer.users, Nodes: f.app.nodeRepository, Published: f.app.deployments}).Proxies(f.ctx, source.Viewer{UserID: account.ID, DeviceID: entriesDevice(t, f, account.ID), NodeGroupIDs: []string{f.groupID}, AllowedProtocols: []string{"vless"}})
	if err != nil || len(proxies) != 0 {
		t.Fatal("explicit server granted subscription", proxies, err)
	}
	visible, err := (&portal.Service{Users: f.app.quotaDeployer.users, Nodes: f.app.nodeRepository}).GrantedNodes(f.ctx, account.ID)
	if err != nil || len(visible) != 0 {
		t.Fatal("explicit server exposed in portal", visible, err)
	}
	for _, ids := range [][]string{{"missing"}, {f.nodeID, f.nodeID}, make([]string, 101)} {
		body["serverIds"] = ids
		if r := f.owner.call("PUT", "/api/v1/node-groups/"+f.groupID, body); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	body["serverIds"] = []string{}
	if r := f.owner.call("PUT", "/api/v1/node-groups/"+f.groupID, body); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	g, err = f.app.nodeRepository.Group(f.ctx, f.groupID)
	if err != nil || len(g.ServerIDs) != 0 || len(g.NodeIDs) != 0 {
		t.Fatal(g, err)
	}
}

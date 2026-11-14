package subscription

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
)

type candidateClones struct{}

type candidateRelay struct{ userID, groupID string }

func (candidateRelay) ID() string { return "test-relay" }

func (s candidateRelay) Proxies(_ context.Context, viewer source.Viewer) ([]proxy.Proxy, []proxy.Warning, error) {
	if viewer.UserID != s.userID {
		return nil, nil, nil
	}
	return []proxy.Proxy{{Name: "已发布中转", Protocol: "socks5", Server: "relay-private.example", Credential: proxy.Credential{Password: "relay-password"}, Origin: proxy.Origin{Source: "published", Relayed: true, Ref: "relay-rule", Node: proxy.Ref{ID: "entry", Name: "中转入口"}, Labels: []string{"nodeGroup:" + s.groupID}}}}, nil, nil
}

func (candidateClones) Chains(_ context.Context, endpoints []proxy.Proxy) ([]proxy.Proxy, []proxy.Warning) {
	clone := endpoints[0]
	clone.Name, clone.ClientChain = "chain-hidden", "chain"
	return append(endpoints, clone), nil
}

func TestCandidatesOwnerMetadataAndSelection(t *testing.T) {
	p := newRulesPanel(t)
	ctx := context.Background()
	p.publish(t, p.nodeIDs[0])
	imports := &importer.Store{Database: p.database, Vault: p.users.Vault}
	p.service.CandidateUsers = p.users
	p.service.ImportNames = imports
	p.service.ClientChains = candidateClones{}
	p.service.Sources.Register(imports)
	p.service.Sources.Register(candidateRelay{userID: p.u.ID, groupID: p.groups[0].ID})
	sourceID, _, err := imports.Save(ctx, "", importer.Input{Name: "导入来源", Content: "socks5://secret-user:secret-password@private-server.example:1000#US-import", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	items, err := imports.Items(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("imported items=%v error=%v", items, err)
	}
	importID := items[0]["id"].(string)
	group := p.groups[0]
	if _, err := p.nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: group.Purpose, Enabled: true, SubscriptionVisible: true, NodeIDs: group.NodeIDs, Imports: []node.GroupImport{{SourceID: sourceID, ProxyKey: "id:" + importID}}}); err != nil {
		t.Fatal(err)
	}
	owner, _ := sessionFor(t, p.database, "Owner")
	viewer, _ := sessionFor(t, p.database, "Viewer")
	for _, cookie := range []*http.Cookie{nil, viewer} {
		r := p.do(t, "GET", "/api/v1/client-policy-candidates", "", cookie, "")
		if r.Code != 401 && r.Code != 403 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	check := func(path string, auto bool) Candidates {
		t.Helper()
		r := p.do(t, "GET", path, "", owner, "")
		var result Candidates
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil {
			t.Fatal(r.Code, r.Body.String())
		}
		if result.User == nil || result.User.ID != p.u.ID || result.User.Name != p.u.Username || result.AutoSelected != auto {
			t.Fatal(r.Body.String())
		}
		for _, forbidden := range []string{"secret-password", "secret-user", "private-server", "relay-password", "relay-private", "credential", "server", "token", "chain-hidden"} {
			if strings.Contains(r.Body.String(), forbidden) {
				t.Fatal("metadata leaked", r.Body.String())
			}
		}
		return result
	}
	result := check("/api/v1/client-policy-candidates", true)
	if len(result.Items) != 4 {
		t.Fatal(result)
	}
	if result.Items[0].Ref == result.Items[1].Ref {
		t.Fatal("two inbounds collapsed")
	}
	found := false
	relayFound := false
	for _, item := range result.Items {
		if len(item.NodeGroupIDs) == 0 {
			t.Fatal("missing group labels", item)
		}
		if item.OriginKind == "import" {
			found = item.Ref == "import:"+sourceID+":id:"+importID && item.OriginName == "导入来源"
		}
		if item.OriginKind == "relay" {
			relayFound = item.Ref == "relay:relay-rule" && item.OriginName == "中转入口"
		}
	}
	if !found || !relayFound {
		t.Fatal(result)
	}
	check("/api/v1/client-policy-candidates?userId="+p.u.ID, false)
	if _, err := p.users.Save(ctx, "", user.Input{Username: "no-grants", Status: "active", AllowedProtocols: []string{"socks5"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("UPDATE users SET status='disabled' WHERE id=?", p.u.ID); err != nil {
		t.Fatal(err)
	}
	r := p.do(t, "GET", "/api/v1/client-policy-candidates", "", owner, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"items":[]`) || !strings.Contains(r.Body.String(), `"diagnostic":`) {
		t.Fatal(r.Code, r.Body.String())
	}
}

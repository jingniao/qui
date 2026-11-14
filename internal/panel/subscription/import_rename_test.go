package subscription

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/rules"
)

func TestImportedRenameHTTPAuthorizationAndResponse(t *testing.T) {
	p := newRulesPanel(t)
	ctx := context.Background()
	imports := &importer.Store{Database: p.database, Vault: p.users.Vault}
	(&importer.Handler{Store: imports, Auth: p.handler.Auth, Audit: p.audit}).Register(p.mux)
	_, _, err := imports.Save(ctx, "", importer.Input{Name: "manual", Interval: 60, Content: "socks5://user:secret@example.com:1080#Original"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := imports.Items(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v error=%v", items, err)
	}
	path := "/api/v1/imported-proxies/" + items[0]["id"].(string)
	owner, csrf := sessionFor(t, p.database, "Owner")
	viewer, viewerCSRF := sessionFor(t, p.database, "Viewer")
	for _, tc := range []struct {
		name, body, csrf string
		cookie           *http.Cookie
		status           int
	}{
		{"viewer", `{"name":"Renamed","expectedRevision":1}`, viewerCSRF, viewer, 403},
		{"missing csrf", `{"name":"Renamed","expectedRevision":1}`, "", owner, 403},
		{"unknown field", `{"name":"Renamed","expectedRevision":1,"protocol":"http"}`, csrf, owner, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := p.do(t, "PUT", path, tc.body, tc.cookie, tc.csrf)
			if response.Code != tc.status {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
	response := p.do(t, "PUT", path, `{"name":"  Renamed  ","expectedRevision":1}`, owner, csrf)
	if response.Code != 200 {
		t.Fatalf("rename=%d %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	response = p.do(t, "GET", "/api/v1/imported-proxies", "", owner, "")
	var list struct{ Items []map[string]any }
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil || len(list.Items) != 1 {
		t.Fatalf("list=%d %s", response.Code, response.Body.String())
	}
	if !reflect.DeepEqual(result, list.Items[0]) || result["name"] != "Renamed" || result["revision"] != float64(2) {
		t.Fatalf("rename response=%v list=%v", result, list.Items)
	}
	if !reflect.DeepEqual(p.audit.actions, []string{"导入节点改名"}) {
		t.Fatalf("audit=%v", p.audit.actions)
	}
	response = p.do(t, "PUT", path, `{"name":"Stale","expectedRevision":1}`, owner, csrf)
	if response.Code != 409 || !strings.Contains(response.Body.String(), `"code":"stale"`) {
		t.Fatalf("stale=%d %s", response.Code, response.Body.String())
	}
}

func TestImportedRenamePreservesReferencesAndSubscription(t *testing.T) {
	p := newRulesPanel(t)
	ctx := context.Background()
	imports := &importer.Store{Database: p.database, Vault: p.users.Vault}
	p.service.CandidateUsers = p.users
	p.service.ImportNames = imports
	p.service.Sources.Register(imports)
	sourceID, _, err := imports.Save(ctx, "", importer.Input{Name: "manual", Interval: 60, Content: "socks5://user:secret@example.com:1080#Original"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := imports.Items(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v error=%v", items, err)
	}
	itemID := items[0]["id"].(string)
	member := node.GroupImport{SourceID: sourceID, ProxyKey: "id:" + itemID}
	group := p.groups[0]
	if _, err := p.nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: "subscription", Enabled: true, SubscriptionVisible: true, Imports: []node.GroupImport{member}}); err != nil {
		t.Fatal(err)
	}
	ref := "import:" + sourceID + ":id:" + itemID
	template := rules.Default()
	template.Groups = []rules.Group{{Name: "Selected", Type: "select", Members: []string{"@endpoint:" + ref}}}
	template.Rules = []rules.Rule{{ID: "final", Type: rules.RuleMatch, Target: "Selected", Enabled: true, Priority: 1}}
	active, err := p.handler.Rules.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.handler.Profiles.WriteProfile(ctx, "", rules.ProfileInput{Name: "stable", Template: template, UIMetadata: json.RawMessage(`{}`), Activate: true, ExpectedActiveRevision: &active.Revision}, false, "Owner"); err != nil {
		t.Fatal(err)
	}
	before, err := p.handler.Rules.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := imports.Rename(ctx, itemID, importer.RenameInput{Name: "Renamed", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	after, err := p.handler.Rules.Read(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("policy changed: before=%+v after=%+v error=%v", before, after, err)
	}
	savedGroup, err := p.nodes.Group(ctx, group.ID)
	if err != nil || !reflect.DeepEqual(savedGroup.Imports, []node.GroupImport{member}) {
		t.Fatalf("group=%+v error=%v", savedGroup, err)
	}
	owner, _ := sessionFor(t, p.database, "Owner")
	response := p.do(t, "GET", "/api/v1/client-policy-candidates?userId="+p.u.ID, "", owner, "")
	var candidates Candidates
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &candidates) != nil || len(candidates.Items) != 1 {
		t.Fatalf("candidates=%d %s", response.Code, response.Body.String())
	}
	if candidates.Items[0].Ref != ref || candidates.Items[0].Name != "Renamed" {
		t.Fatalf("candidate=%+v", candidates.Items[0])
	}
	_, token, err := p.service.Rotate(ctx, p.u.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	response = p.do(t, "GET", "/sub/"+token+"?format=clash", "", nil, "")
	var document struct {
		Proxies []struct{ Name, Username, Password string }
		Groups  []struct {
			Name    string
			Proxies []string
		} `yaml:"proxy-groups"`
	}
	if response.Code != 200 || yaml.Unmarshal(response.Body.Bytes(), &document) != nil {
		t.Fatalf("subscription=%d %s", response.Code, response.Body.String())
	}
	if len(document.Proxies) != 1 || document.Proxies[0].Name != "Renamed" || document.Proxies[0].Username != "user" || document.Proxies[0].Password != "secret" {
		t.Fatalf("subscription endpoints=%+v", document.Proxies)
	}
	if len(document.Groups) != 1 || document.Groups[0].Name != "Selected" || !reflect.DeepEqual(document.Groups[0].Proxies, []string{"Renamed"}) {
		t.Fatalf("subscription groups=%+v", document.Groups)
	}
}

package subscription

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/rules"
)

func TestImportedIdentityCandidatesSubscriptionAndProfilePreview(t *testing.T) {
	p := newRulesPanel(t)
	ctx := context.Background()
	imports := &importer.Store{Database: p.database, Vault: p.users.Vault}
	p.service.CandidateUsers = p.users
	p.service.ImportNames = imports
	p.service.Sources.Register(imports)
	sourceID, _, err := imports.Save(ctx, "", importer.Input{
		Name: "manual nodes", Interval: 60,
		Content: "socks5://allowed-user:allowed-password@allowed.example:1080#Allowed\nsocks5://hidden-user:hidden-password@hidden.example:2080#Hidden",
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := imports.Items(ctx)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%v error=%v", items, err)
	}
	ids := map[string]string{}
	for _, item := range items {
		itemID := item["id"].(string)
		ids[item["name"].(string)] = itemID
		if item["sourceId"] != sourceID || item["proxyKey"] != "id:"+itemID {
			t.Fatalf("content item has no stable member key: %v", item)
		}
	}
	if ids["Allowed"] == "" || ids["Hidden"] == "" {
		t.Fatalf("missing imported nodes: %v", ids)
	}
	for _, group := range p.groups[:2] {
		member := node.GroupImport{SourceID: sourceID, ProxyKey: "id:" + ids["Allowed"]}
		saved, err := p.nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: "subscription", Enabled: true, SubscriptionVisible: true, Imports: []node.GroupImport{member}})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(saved.Imports, []node.GroupImport{member}) {
			t.Fatalf("saved members=%v", saved.Imports)
		}
	}
	allowedRef := "import:" + sourceID + ":id:" + ids["Allowed"]
	hiddenRef := "import:" + sourceID + ":id:" + ids["Hidden"]
	owner, csrf := sessionFor(t, p.database, "Owner")
	response := p.do(t, "GET", "/api/v1/client-policy-candidates?userId="+p.u.ID, "", owner, "")
	var candidates Candidates
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &candidates) != nil || len(candidates.Items) != 1 {
		t.Fatalf("candidates: %d %s", response.Code, response.Body.String())
	}
	item := candidates.Items[0]
	if item.Ref != allowedRef || item.Name != "Allowed" || item.OriginKind != "import" {
		t.Fatalf("candidate identity=%+v", item)
	}
	wantGroups := []string{p.groups[0].ID, p.groups[1].ID}
	sort.Strings(wantGroups)
	sort.Strings(item.NodeGroupIDs)
	if !reflect.DeepEqual(item.NodeGroupIDs, wantGroups) {
		t.Fatalf("candidate group labels=%v, want %v", item.NodeGroupIDs, wantGroups)
	}
	for _, secret := range []string{"allowed-user", "allowed-password", "allowed.example", "Hidden", "hidden-user", "hidden-password", "hidden.example", ids["Hidden"]} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("candidate metadata leaked %q", secret)
		}
	}

	template := rules.Default()
	template.Groups = []rules.Group{
		{Name: "Selected", Type: "select", Members: []string{"@endpoint:" + allowedRef}},
		{Name: "Unauthorized", Type: "select", Members: []string{"@endpoint:" + hiddenRef}, EmptyPolicy: "reject"},
	}
	template.Rules = []rules.Rule{{ID: "final", Type: rules.RuleMatch, Target: "Selected", Enabled: true, Priority: 1}}
	active, err := p.handler.Rules.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	saved, _, err := p.handler.Profiles.WriteProfile(ctx, "", rules.ProfileInput{Name: "stable imports", Template: template, UIMetadata: json.RawMessage(`{}`), Activate: true, ExpectedActiveRevision: &active.Revision}, false, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Profile.Template.Groups[0].Members; !reflect.DeepEqual(got, []string{"@endpoint:" + allowedRef}) {
		t.Fatalf("saved policy member=%v", got)
	}
	previewBody, err := json.Marshal(map[string]any{"profileId": saved.Profile.ID, "profileRevision": saved.Profile.Revision, "userId": p.u.ID})
	if err != nil {
		t.Fatal(err)
	}
	response = p.do(t, "POST", "/api/v1/client-policy-profiles/preview", string(previewBody), owner, csrf)
	var preview profilePreviewResult
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &preview) != nil {
		t.Fatalf("profile preview: %d %s", response.Code, response.Body.String())
	}
	if preview.PreviewSource != "saved" || preview.ProfileID != saved.Profile.ID || len(preview.Groups) != 2 {
		t.Fatalf("preview source=%+v", preview)
	}
	wantMembers := [][]previewMember{
		{{Kind: rules.KindProxy, Name: "Allowed", Ref: allowedRef}},
		{{Kind: rules.KindBuiltin, Name: rules.TargetReject}},
	}
	for i, group := range preview.Groups {
		if !reflect.DeepEqual(group.Members, wantMembers[i]) {
			t.Fatalf("preview group %s=%v, want %v", group.Name, group.Members, wantMembers[i])
		}
	}
	_, token, err := p.service.Rotate(ctx, p.u.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	response = p.do(t, "GET", "/sub/"+token+"?format=clash", "", nil, "")
	if response.Code != 200 {
		t.Fatalf("subscription: %d %s", response.Code, response.Body.String())
	}
	var realYAML, previewYAML map[string]any
	if err := yaml.Unmarshal(response.Body.Bytes(), &realYAML); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(preview.Body), &previewYAML); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(realYAML, previewYAML) {
		t.Fatal("saved profile preview differs from token subscription")
	}
	var document mihomoDocument
	if err := yaml.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Proxies) != 1 || document.Proxies[0].Name != "Allowed" {
		t.Fatalf("subscription endpoints=%v", document.Proxies)
	}
	for _, forbidden := range []string{"Hidden", "hidden-user", "hidden-password", "hidden.example", ids["Hidden"]} {
		if strings.Contains(response.Body.String(), forbidden) || strings.Contains(preview.Body, forbidden) {
			t.Fatalf("unauthorized node leaked %q", forbidden)
		}
	}
}

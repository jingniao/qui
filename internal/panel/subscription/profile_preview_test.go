package subscription

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"proxycontrol/internal/panel/subscription/rules"
)

func TestProfilePreview(t *testing.T) {
	p := newRulesPanel(t)
	p.service.CandidateUsers = p.users
	p.publish(t, p.nodeIDs[0])
	ctx := context.Background()
	owner, csrf := sessionFor(t, p.database, "Owner")
	viewer, viewerCSRF := sessionFor(t, p.database, "Viewer")
	const path = "/api/v1/client-policy-profiles/preview"
	for _, access := range []struct {
		cookie *http.Cookie
		csrf   string
		status int
	}{{nil, "", 401}, {viewer, viewerCSRF, 403}, {owner, "", 403}} {
		if r := p.do(t, "POST", path, `{}`, access.cookie, access.csrf); r.Code != access.status {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	encode := func(input any) string {
		t.Helper()
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	template := rules.Presets()[1].Template
	template.Groups = append(template.Groups, rules.Group{Name: "空地区", Type: "select", Members: []string{"@all"}, IncludeKeywords: []string{"no-match"}, EmptyPolicy: "reject"})
	template.Rules = append([]rules.Rule{
		{ID: "service", Type: rules.RuleSet, Payload: "service:svc-on", Target: "PROXY", Enabled: true, Priority: 1},
		{ID: "plain", Type: rules.RuleDomainSuffix, Payload: "example.org", Target: "空地区", Enabled: true, Priority: 2},
	}, template.Rules...)
	preview := func(input any) profilePreviewResult {
		t.Helper()
		response := p.do(t, "POST", path, encode(input), owner, csrf)
		var result profilePreviewResult
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatal(response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("preview must not be cached")
		}
		return result
	}
	assertTokens := func(count int) {
		t.Helper()
		var actual int
		var fetched string
		if err := p.database.QueryRow("SELECT count(*), coalesce(max(last_fetched_at),'') FROM subscription_tokens").Scan(&actual, &fetched); err != nil {
			t.Fatal(err)
		}
		if actual != count || fetched != "" {
			t.Fatal("preview changed tokens", actual, fetched)
		}
	}
	var count int
	if err := p.database.QueryRow("SELECT count(*) FROM subscription_tokens").Scan(&count); err != nil {
		t.Fatal(err)
	}
	before, err := p.handler.Rules.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	draft := preview(map[string]any{"template": template})
	if draft.PreviewSource != "draft" || draft.Format != "clash" || draft.Context.User.ID != p.u.ID || !draft.Context.AutoSelected || draft.ActiveRevision != before.Revision || len(draft.TemplateDigest) != 64 || len(draft.Warnings) == 0 {
		t.Fatal(draft)
	}
	assertTokens(count)
	// Every returned group is taken from the expansion used by this YAML body.
	var document struct {
		Groups []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal([]byte(draft.Body), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Groups) != len(draft.Groups) {
		t.Fatal(document, draft.Groups)
	}
	for index, group := range draft.Groups {
		names := []string{}
		for _, member := range group.Members {
			names = append(names, member.Name)
			if member.Kind == rules.KindProxy && !strings.HasPrefix(member.Ref, "inbound:") {
				t.Fatalf("preview proxy has no stable endpoint reference: %+v", member)
			}
			if member.Kind != rules.KindProxy && member.Ref != "" {
				t.Fatalf("non-proxy member has endpoint reference: %+v", member)
			}
		}
		if group.Name != document.Groups[index].Name || !reflect.DeepEqual(names, document.Groups[index].Proxies) {
			t.Fatal(group, document.Groups[index])
		}
	}
	after, err := p.handler.Rules.Read(ctx)
	if err != nil || !reflect.DeepEqual(before, after) || len(p.audit.actions) != 0 {
		t.Fatal("preview wrote state", err)
	}
	saved, _, err := p.handler.Profiles.WriteProfile(ctx, "", rules.ProfileInput{Name: "未启用", Template: template, UIMetadata: json.RawMessage(`{}`)}, false, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	result := preview(map[string]any{"profileId": saved.Profile.ID, "profileRevision": saved.Profile.Revision, "userId": p.u.ID})
	if result.PreviewSource != "saved" || result.ProfileID != saved.Profile.ID || result.ProfileRevision != saved.Profile.Revision || result.Context.AutoSelected || result.Body != draft.Body || result.TemplateDigest != draft.TemplateDigest {
		t.Fatal(result)
	}
	activePreview := p.do(t, "GET", "/api/v1/users/"+p.u.ID+"/subscription-preview?format=clash", "", owner, "")
	var activeBody struct{ Body string }
	if activePreview.Code != 200 || json.Unmarshal(activePreview.Body.Bytes(), &activeBody) != nil || activeBody.Body == draft.Body {
		t.Fatal("inactive preview changed the legacy active preview", activePreview.Body.String())
	}
	// Empty lists and omitted lists have the same normalized digest.
	normalized := template
	normalized.Providers = []rules.Provider{}
	normalized.DNS.Fallback = []string{}
	if same := preview(map[string]any{"template": normalized}); same.TemplateDigest != draft.TemplateDigest {
		t.Fatal("digest depends on nil versus empty lists")
	}
	stale := p.do(t, "POST", path, encode(map[string]any{"profileId": saved.Profile.ID, "profileRevision": saved.Profile.Revision + 1}), owner, csrf)
	if stale.Code != 409 || !strings.Contains(stale.Body.String(), "stale_client_policy") || !strings.Contains(stale.Body.String(), `"revision":1`) {
		t.Fatal(stale.Code, stale.Body.String())
	}
	invalid := template
	invalid.Client.Mode = "invalid"
	failure := p.do(t, "POST", path, encode(map[string]any{"template": invalid}), owner, csrf)
	if failure.Code != 400 || !strings.Contains(failure.Body.String(), "invalid_rules") || !strings.Contains(failure.Body.String(), `"field":"client.mode"`) {
		t.Fatal(failure.Code, failure.Body.String())
	}
	for _, input := range []any{map[string]any{}, map[string]any{"profileId": saved.Profile.ID}, map[string]any{"template": template, "profileRevision": 1}, map[string]any{"template": template, "profileId": saved.Profile.ID, "profileRevision": 1}, map[string]any{"template": template, "unknown": true}} {
		response := p.do(t, "POST", path, encode(input), owner, csrf)
		if response.Code != 400 || !strings.Contains(response.Body.String(), "invalid_request") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	_, token, err := p.service.Rotate(ctx, p.u.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var tokenCount int
	if err := p.database.QueryRow("SELECT count(*) FROM subscription_tokens").Scan(&tokenCount); err != nil {
		t.Fatal(err)
	}
	preview(map[string]any{"template": template, "userId": p.u.ID})
	assertTokens(tokenCount)
	// Set the same template active, then exercise the actual /sub HTTP route.
	if _, _, err := p.handler.Rules.Save(ctx, template, before.Revision, "Owner"); err != nil {
		t.Fatal(err)
	}
	real := p.do(t, "GET", "/sub/"+token+"?format=clash", "", nil, "")
	if real.Code != 200 {
		t.Fatal(real.Code, real.Body.String())
	}
	// Only the provider's URL differs: all rules, groups, proxies and client settings must match.
	var realYAML, previewYAML map[string]any
	if err := yaml.Unmarshal(real.Body.Bytes(), &realYAML); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(draft.Body), &previewYAML); err != nil {
		t.Fatal(err)
	}
	provider := rules.ServiceProviderName("svc-on")
	realProvider := realYAML["rule-providers"].(map[string]any)[provider].(map[string]any)
	previewProvider := previewYAML["rule-providers"].(map[string]any)[provider].(map[string]any)
	if realProvider["url"] == previewProvider["url"] {
		t.Fatal("provider must use a placeholder in preview")
	}
	realProvider["url"] = previewProvider["url"]
	if !reflect.DeepEqual(realYAML, previewYAML) {
		t.Fatal("draft preview differs from real subscription", realYAML, previewYAML)
	}
	// Legacy preview still reads the active template, not an inactive saved profile or draft.
	legacy, err := p.service.Preview(ctx, p.u.ID, "", "clash")
	if err != nil || string(legacy.Body) != draft.Body {
		t.Fatal("legacy active preview", err)
	}
	// A saved template remains readable when its shared provider is disabled later.
	sets := p.handler.ServiceRules.(stubServiceRules)
	p.service.ServiceSetEnabled = func(_ context.Context, id string) bool { return sets[id].Enabled }
	sets["svc-on"] = ServiceRule{Enabled: false}
	disabled := preview(map[string]any{"profileId": saved.Profile.ID, "profileRevision": saved.Profile.Revision})
	found := false
	for _, warning := range disabled.Warnings {
		if strings.Contains(warning.Message, "svc-on") && strings.Contains(warning.Message, "404") && strings.Contains(warning.Message, "缓存") {
			found = true
		}
	}
	if !found || !strings.Contains(disabled.Body, rules.ServiceProviderName("svc-on")) {
		t.Fatal(disabled)
	}
	if response := p.do(t, "GET", "/sub/"+token+"/rules/svc-on", "", nil, ""); response.Code != 404 {
		t.Fatal(response.Code, response.Body.String())
	}
	delete(sets, "svc-on")
	missing := preview(map[string]any{"profileId": saved.Profile.ID, "profileRevision": saved.Profile.Revision})
	if !reflect.DeepEqual(disabled.Warnings, missing.Warnings) {
		t.Fatal(missing.Warnings)
	}
	if _, err := p.database.Exec("UPDATE users SET status='disabled'"); err != nil {
		t.Fatal(err)
	}
	noUser := preview(map[string]any{"template": template})
	if noUser.Body != "" || noUser.Context.User != nil || !noUser.Context.AutoSelected || noUser.Diagnostic != noPreviewUser || len(noUser.Groups) != 0 {
		t.Fatal(noUser)
	}
	failure = p.do(t, "POST", path, encode(map[string]any{"template": invalid}), owner, csrf)
	if failure.Code != 400 || !strings.Contains(failure.Body.String(), "invalid_rules") {
		t.Fatal("no-user preview must still validate", failure.Body.String())
	}
}

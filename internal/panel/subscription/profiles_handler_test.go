package subscription

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/rules"
)

func TestProfilesOwnerCSRFAndLegacyCompatibility(t *testing.T) {
	p := newRulesPanel(t)
	owner, csrf := sessionFor(t, p.database, "Owner")
	viewer, viewerCSRF := sessionFor(t, p.database, "Viewer")
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/client-policy-profiles"},
		{"GET", "/api/v1/client-policy-profiles/missing"},
		{"POST", "/api/v1/client-policy-profiles"},
		{"PUT", "/api/v1/client-policy-profiles/missing"},
		{"DELETE", "/api/v1/client-policy-profiles/missing"},
		{"POST", "/api/v1/client-policy-profiles/missing/activate"},
	} {
		if r := p.do(t, route.method, route.path, `{}`, viewer, viewerCSRF); r.Code != 403 {
			t.Fatal(route, r.Code)
		}
		if route.method != "GET" {
			if r := p.do(t, route.method, route.path, `{}`, owner, ""); r.Code != 403 {
				t.Fatal("CSRF", route, r.Code)
			}
		}
	}
	var list rules.ProfileList
	r := p.do(t, "GET", "/api/v1/client-policy-profiles?page=1&pageSize=1", "", owner, "")
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &list) != nil || list.Total != 1 || strings.Contains(r.Body.String(), `"template"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := p.do(t, "GET", "/api/v1/client-policy-profiles?page=0", "", owner, ""); r.Code != 400 {
		t.Fatal(r.Code)
	}
	if r := p.do(t, "PUT", "/api/v1/subscription-rules", saveBody(t, rules.Presets()[1].Template, 0), owner, csrf); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var state rules.ProfileResult
	r = p.do(t, "GET", "/api/v1/client-policy-profiles/"+list.ActiveProfileID, "", owner, "")
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &state) != nil || state.Profile.Template.Preset != "minimal" || state.ActiveRevision != 1 {
		t.Fatal(r.Code, r.Body.String())
	}
	input := rules.ProfileInput{Name: "编辑模板", Description: "description", Template: rules.Presets()[2].Template, UIMetadata: json.RawMessage(`{"x":{"opaque":[1,true,"保留"]}}`), ExpectedRevision: state.Profile.Revision, ExpectedActiveRevision: &state.ActiveRevision}
	encode := func(value any) string {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	r = p.do(t, "PUT", "/api/v1/client-policy-profiles/"+state.Profile.ID, encode(input), owner, csrf)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var snapshot rules.Stored
	r = p.do(t, "GET", "/api/v1/subscription-rules", "", viewer, "")
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &snapshot) != nil || snapshot.Template.Preset != "adblock" || snapshot.Revision != 2 || strings.Contains(r.Body.String(), "opaque") {
		t.Fatal(r.Code, r.Body.String())
	}
	r = p.do(t, "PUT", "/api/v1/client-policy-profiles/"+state.Profile.ID, encode(input), owner, csrf)
	if r.Code != 409 || !strings.Contains(r.Body.String(), "stale_client_policy") || !strings.Contains(r.Body.String(), `"revision":3`) {
		t.Fatal(r.Code, r.Body.String())
	}
	input.ExpectedRevision++
	r = p.do(t, "PUT", "/api/v1/client-policy-profiles/"+state.Profile.ID, encode(input), owner, csrf)
	if r.Code != 409 || !strings.Contains(r.Body.String(), `"activeRevision":2`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := p.do(t, "DELETE", "/api/v1/client-policy-profiles/"+state.Profile.ID+"?expectedRevision=3", "", owner, csrf); r.Code != 409 || !strings.Contains(r.Body.String(), "client_policy_active") {
		t.Fatal(r.Code, r.Body.String())
	}
	input.ExpectedActiveRevision = nil
	input.Activate = false
	key := "legacy:hash"
	input.ImportKey = &key
	r = p.do(t, "POST", "/api/v1/client-policy-profiles", encode(input), owner, csrf)
	var imported rules.ProfileResult
	if r.Code != http.StatusCreated || json.Unmarshal(r.Body.Bytes(), &imported) != nil || imported.ActiveProfileID != state.Profile.ID {
		t.Fatal(r.Code, r.Body.String())
	}
	r = p.do(t, "POST", "/api/v1/client-policy-profiles", encode(input), owner, csrf)
	var repeated rules.ProfileResult
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &repeated) != nil || repeated.Profile.ID != imported.Profile.ID {
		t.Fatal(r.Code, r.Body.String())
	}
	r = p.do(t, "POST", "/api/v1/client-policy-profiles/"+imported.Profile.ID+"/activate", `{"expectedRevision":1,"expectedActiveRevision":2}`, owner, csrf)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"activeRevision":3`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := p.do(t, "DELETE", "/api/v1/client-policy-profiles/"+state.Profile.ID+"?expectedRevision=3", "", owner, csrf); r.Code != 204 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestProfilesValidationAndAtomicCreateActivate(t *testing.T) {
	p := newRulesPanel(t)
	owner, csrf := sessionFor(t, p.database, "Owner")
	for _, test := range []struct {
		name     string
		metadata json.RawMessage
		template rules.Template
		field    string
	}{
		{"", json.RawMessage(`{}`), rules.Default(), "name"},
		{strings.Repeat("字", 65), json.RawMessage(`{}`), rules.Default(), "name"},
		{"name", json.RawMessage(`[]`), rules.Default(), "uiMetadata"},
		{"name", json.RawMessage(`null`), rules.Default(), "uiMetadata"},
		{"name", json.RawMessage(`{"text":"` + strings.Repeat("x", 64<<10) + `"}`), rules.Default(), "uiMetadata"},
		{"name", json.RawMessage(`{}`), rules.Template{}, "client.mode"},
	} {
		input, _ := json.Marshal(rules.ProfileInput{Name: test.name, UIMetadata: test.metadata, Template: test.template})
		r := p.do(t, "POST", "/api/v1/client-policy-profiles", string(input), owner, csrf)
		if r.Code != 400 || !strings.Contains(r.Body.String(), "invalid_rules") || !strings.Contains(r.Body.String(), test.field) {
			t.Fatal(test.field, r.Code, r.Body.String())
		}
	}
	input := rules.ProfileInput{Name: "new active", Template: rules.Presets()[1].Template, UIMetadata: json.RawMessage(`{}`), Activate: true}
	bad := int64(99)
	input.ExpectedActiveRevision = &bad
	data, _ := json.Marshal(input)
	r := p.do(t, "POST", "/api/v1/client-policy-profiles", string(data), owner, csrf)
	if r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	bad = 0
	data, _ = json.Marshal(input)
	r = p.do(t, "POST", "/api/v1/client-policy-profiles", string(data), owner, csrf)
	var created rules.ProfileResult
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &created) != nil || created.Profile.ID != created.ActiveProfileID || created.ActiveRevision != 1 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = p.do(t, "GET", "/api/v1/client-policy-profiles", "", owner, "")
	var list rules.ProfileList
	if json.Unmarshal(r.Body.Bytes(), &list) != nil || list.Total != 2 {
		t.Fatal(r.Body.String())
	}
}

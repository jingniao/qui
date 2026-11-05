package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestServiceSetReferencesAndProtection(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	repo := &policy.Repository{Database: app.Database, ClientReferences: clientServiceReferences{}}
	store := &rules.Store{Database: app.Database, Lookup: ruleLookup{policies: repo}}
	for _, kind := range []string{"client", "node", "both"} {
		t.Run(kind, func(t *testing.T) {
			input := policy.ServiceInput{Name: "共享 " + kind, Enabled: true, Domains: []string{"example.org"}}
			set, err := repo.SaveService(ctx, "", input)
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/service-sets/" + set.ID
			if kind != "node" {
				template := rules.Presets()[1].Template
				template.Rules = append([]rules.Rule{
					{ID: "on", Type: rules.RuleSet, Payload: "service:" + set.ID, Target: "PROXY", Enabled: true, Priority: 1},
					{ID: "off", Type: rules.RuleSet, Payload: "service:" + set.ID, Target: "PROXY", Enabled: false, Priority: 2},
				}, template.Rules...)
				active, err := store.Read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if response := owner.call("PUT", "/api/v1/subscription-rules", map[string]any{"template": template, "expectedRevision": active.Revision}); response.Code != 200 {
					t.Fatal(response.Code, response.Body.String())
				}
				if _, _, err := store.WriteProfile(ctx, "", rules.ProfileInput{Name: "未启用 " + kind, Template: template, UIMetadata: json.RawMessage(`{}`)}, false, "Owner"); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "client" {
				for _, enabled := range []bool{true, false} {
					if _, err := repo.Save(ctx, "", policy.Input{Name: "节点引用", Enabled: enabled, Priority: 1, Match: desiredstate.Match{ServiceIDs: []string{set.ID}}, Action: desiredstate.Action{Type: "Block"}}); err != nil {
						t.Fatal(err)
					}
				}
			}
			response := owner.call("GET", path+"/references", nil)
			var refs policy.ServiceReferences
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &refs) != nil {
				t.Fatal(response.Code, response.Body.String())
			}
			if (len(refs.NodePolicies) == 2) != (kind != "client") || (len(refs.ClientProfiles) == 2) != (kind != "node") {
				t.Fatal(refs)
			}
			if kind != "node" {
				active := 0
				for _, profile := range refs.ClientProfiles {
					if profile.Active {
						active++
					}
					if len(profile.Rules) != 2 || !profile.Rules[0].Enabled || profile.Rules[1].Enabled {
						t.Fatal(profile)
					}
				}
				if active != 1 {
					t.Fatal(refs)
				}
			}
			blocked := owner.call("DELETE", path, nil)
			var body struct {
				Error struct {
					Code string
					policy.ServiceReferences
				}
			}
			if blocked.Code != 409 || json.Unmarshal(blocked.Body.Bytes(), &body) != nil || body.Error.Code != "service_set_in_use" || len(body.Error.NodePolicies) != len(refs.NodePolicies) || len(body.Error.ClientProfiles) != len(refs.ClientProfiles) {
				t.Fatal(blocked.Code, blocked.Body.String())
			}
			input.Enabled = false
			disabled := owner.call("PUT", path, input)
			var saved policy.ServiceSet
			if disabled.Code != 200 || json.Unmarshal(disabled.Body.Bytes(), &saved) != nil || saved.Enabled || saved.References == nil || !strings.Contains(saved.Warning, "缓存") {
				t.Fatal(disabled.Code, disabled.Body.String())
			}
			if kind != "node" {
				active, err := store.Read(ctx)
				if err != nil {
					t.Fatal("disabled reference broke structural read", err)
				}
				failure := owner.call("PUT", "/api/v1/subscription-rules", map[string]any{"template": active.Template, "expectedRevision": active.Revision})
				if failure.Code != 400 || !strings.Contains(failure.Body.String(), "invalid_rules") || !strings.Contains(failure.Body.String(), "已禁用") {
					t.Fatal(failure.Code, failure.Body.String())
				}
			}
		})
	}
	path := "/api/v1/service-sets/missing/references"
	if r := owner.call("GET", path, nil); r.Code != 404 {
		t.Fatal(r.Code)
	}
	if r := (budgetHTTP{app: app}).call("GET", path, nil); r.Code != 401 {
		t.Fatal(r.Code)
	}
	if _, err := app.Database.Exec("UPDATE admins SET role='Viewer'"); err != nil {
		t.Fatal(err)
	}
	if r := owner.call("GET", path, nil); r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
}

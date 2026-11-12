package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/policy"
	"proxycontrol/pkg/contracts/desiredstate"
)

type ruleSourceFetcher struct{}

func (ruleSourceFetcher) Fetch(context.Context, string) ([]byte, error) {
	return []byte("DOMAIN,chatgpt.com\n"), nil
}

func TestAssembledPolicySaveChecksReportedKeywordCapabilities(t *testing.T) {
	t.Parallel()
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	nodes := &node.Repository{Database: app.Database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "keyword node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	input := policy.Input{Name: "disabled keyword", Enabled: false, Scope: desiredstate.Scope{NodeIDs: []string{n.ID}}, Match: desiredstate.Match{Keywords: []string{"openai"}, Network: "all"}, Action: desiredstate.Action{Type: "Block", Failover: "Block"}, Timezone: "UTC"}
	for _, capable := range []bool{false, true} {
		caps := `{"multiCoreSupported":true,"cores":[{"name":"xray","domainKeywordSupported":true},{"name":"sing-box","domainKeywordSupported":false}]}`
		if capable {
			caps = strings.ReplaceAll(caps, `:false`, `:true`)
		}
		if _, err := app.Database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=? WHERE id=?", caps, n.ID); err != nil {
			t.Fatal(err)
		}
		response := owner.call("POST", "/api/v1/policies", input)
		if capable {
			if response.Code != 201 {
				t.Fatalf("capable cores rejected: %d %s", response.Code, response.Body.String())
			}
		} else if response.Code == 201 || !strings.Contains(response.Body.String(), "keyword_unsupported") {
			t.Fatalf("old core accepted: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestAssembledPolicyRepositoryResolvesRefreshedSources(t *testing.T) {
	t.Parallel()
	app := testServer(t)
	ctx := context.Background()
	repo := app.sourceRefresher.Repository
	nodes := &node.Repository{Database: app.Database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "source node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sources := []policy.SourceInput{{URL: "https://example.com/rules", Format: "text", Behavior: "classical"}}
	set, err := repo.SaveService(ctx, "", policy.ServiceInput{Name: "external", Enabled: true, Sources: &sources})
	if err != nil {
		t.Fatal(err)
	}
	app.sourceRefresher.Fetcher = ruleSourceFetcher{}
	if _, err := app.sourceRefresher.Refresh(ctx, set.Sources[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Save(ctx, "", policy.Input{Name: "block external", Enabled: true, Match: desiredstate.Match{ServiceIDs: []string{set.ID}, Network: "all"}, Action: desiredstate.Action{Type: "Block", Failover: "Block"}, Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	service := &policy.Service{Repository: repo, Nodes: nodes}
	rules, err := service.ForNode(ctx, n.ID)
	if err != nil || len(rules) != 1 || !rules[0].Enabled || len(rules[0].Match.Domains) != 1 || rules[0].Match.Domains[0] != "chatgpt.com" {
		t.Fatalf("external rules missing: %+v %v", rules, err)
	}
	if _, err := app.Database.ExecContext(ctx, "UPDATE service_set_sources SET encrypted_cache=?", []byte("invalid")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ForNode(ctx, n.ID); err == nil {
		t.Fatal("corrupt cache silently accepted")
	}
}

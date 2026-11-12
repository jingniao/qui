package policy

import (
	"context"
	"encoding/json"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	shared "proxycontrol/pkg/contracts/policy"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Service, string, user.User, user.Device, node.Group) {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	database, err := dbtest.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	configurationVault, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Policy node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&proxy.Service{Repository: &proxy.Repository{Database: database, Vault: configurationVault}, Nodes: nodes}).SaveInbound(ctx, "", proxy.InboundInput{Name: "policy entry", NodeID: n.ID, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Policy group", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{n.ID}})
	if err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: database, Vault: configurationVault}
	u, err := users.Save(ctx, "", user.Input{Username: "policy_user", Status: "active", AllowedProtocols: []string{"socks5"}, NodeGroupIDs: []string{group.ID}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Policy laptop", Protocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Repository: &Repository{Database: database, Vault: configurationVault}, Nodes: nodes, Users: users, Proxies: &proxy.Repository{Database: database, Vault: configurationVault}, Settings: &system.Repository{Database: database}}, n.ID, u, device, group
}
func TestServiceSetsExpandAtPublicationAndMatchSimulator(t *testing.T) {
	service, nodeID, u, _, group := fixture(t)
	ctx := context.Background()
	rule, err := service.Save(ctx, "", Input{Name: "Netflix blocked", Priority: 10, Enabled: true, Scope: desiredstate.Scope{UserIDs: []string{u.ID}, GroupIDs: []string{group.ID}}, Match: desiredstate.Match{ServiceIDs: []string{"service-netflix"}, Ports: []string{"443"}, Network: "tcp"}, Action: desiredstate.Action{Type: "Block"}})
	if err != nil {
		t.Fatal(err)
	}
	request := shared.Request{NodeID: nodeID, UserID: u.ID, Domain: "www.netflix.com", Port: 443, Network: "tcp", Time: time.Now().UTC()}
	decision, err := service.Simulate(ctx, request)
	if err != nil || !decision.Matched || decision.PolicyID != rule.ID {
		t.Fatal("simulator did not match expanded service", err)
	}
	snapshot, err := service.ForNode(ctx, nodeID)
	if err != nil || len(snapshot) != 1 {
		t.Fatal(err)
	}
	if len(snapshot[0].Match.ServiceIDs) != 0 || len(snapshot[0].Scope.GroupIDs) != 0 || len(snapshot[0].Match.Suffixes) == 0 {
		t.Fatal("groups or services not expanded")
	}
	before, _ := json.Marshal(snapshot)
	builtin, err := service.Repository.Service(ctx, "service-netflix")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Repository.SaveService(ctx, builtin.ID, ServiceInput{Name: builtin.Name, Enabled: true, Suffixes: []string{"new-netflix.test"}}); err != nil {
		t.Fatal(err)
	}
	if beforeAgain, _ := json.Marshal(snapshot); string(beforeAgain) != string(before) {
		t.Fatal("service draft mutation altered existing snapshot")
	}
	decision, err = service.Simulate(ctx, request)
	if err != nil || decision.Matched {
		t.Fatal("updated draft did not affect next simulation", err)
	}
	request.Domain = "new-netflix.test"
	decision, err = service.Simulate(ctx, request)
	if err != nil || !decision.Matched {
		t.Fatal("updated service draft missing", err)
	}
	if err := service.Repository.DeleteService(ctx, builtin.ID); err == nil {
		t.Fatal("builtin deletion accepted")
	}
}

// v1.1 removed the device scope from policies and the simulator: both refuse it.
func TestPolicyReferencesDefaultAndDeviceScopeRemoval(t *testing.T) {
	service, nodeID, u, device, _ := fixture(t)
	ctx := context.Background()
	if _, err := service.Save(ctx, "", Input{Name: "Unknown user", Enabled: true, Scope: desiredstate.Scope{UserIDs: []string{"missing"}}, Action: desiredstate.Action{Type: "Block"}}); err == nil {
		t.Fatal("unknown user scope accepted")
	}
	if _, err := service.Save(ctx, "", Input{Name: "Device scope", Enabled: true, Scope: desiredstate.Scope{DeviceIDs: []string{device.ID}}, Action: desiredstate.Action{Type: "Block"}}); err == nil {
		t.Fatal("a device scope was accepted")
	}
	decision, err := service.Simulate(ctx, shared.Request{NodeID: nodeID, UserID: u.ID, Domain: "example.test", Port: 80, Network: "tcp"})
	if err != nil || decision.Matched || decision.Action.Type != "Direct" {
		t.Fatal("default rule incorrect", err)
	}
	if _, err := service.Simulate(ctx, shared.Request{NodeID: nodeID, UserID: u.ID, DeviceID: device.ID, Domain: "example.test", Port: 80, Network: "tcp"}); err == nil {
		t.Fatal("a device simulation was accepted")
	}
	custom, err := service.Repository.SaveService(ctx, "", ServiceInput{Name: "Custom development", Enabled: true, Domains: []string{"git.example.test"}, CIDRs: []string{"192.0.2.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Save(ctx, "", Input{Name: "Custom service block", Enabled: true, Match: desiredstate.Match{ServiceIDs: []string{custom.ID}}, Action: desiredstate.Action{Type: "Block"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Repository.DeleteService(ctx, custom.ID); err == nil {
		t.Fatal("referenced service deletion accepted")
	}
}

func TestExplicitServerGroupPolicyScope(t *testing.T) {
	service, nodeID, _, _, _ := fixture(t)
	ctx := context.Background()
	for _, purpose := range []string{"relay", "egress", "subscription", "mixed"} {
		g, err := (&node.Repository{Database: service.Repository.Database}).SaveGroup(ctx, "", node.GroupInput{Name: "explicit " + purpose, Purpose: purpose, Enabled: true, SubscriptionVisible: true, ServerIDs: []string{nodeID}})
		if err != nil {
			t.Fatal(err)
		}
		rule, err := service.Save(ctx, "", Input{Name: "scope " + purpose, Priority: 10, Enabled: true, Scope: desiredstate.Scope{GroupIDs: []string{g.ID}}, Match: desiredstate.Match{Domains: []string{"example.com"}}, Action: desiredstate.Action{Type: "Block"}})
		if err != nil {
			t.Fatal(err)
		}
		policies, err := service.ForNode(ctx, nodeID)
		found := false
		for _, p := range policies {
			if p.ID == rule.ID {
				found = true
			}
		}
		if err != nil || !found {
			t.Fatal("explicit server lost group policy", purpose, policies, err)
		}
	}
}

func TestResolveMergesKeywordsAndSources(t *testing.T) {
	service, _, _, _, _ := fixture(t)
	ctx := context.Background()

	svc, err := service.Repository.SaveService(ctx, "", ServiceInput{
		Name:     "Resolve Svc",
		Enabled:  true,
		Keywords: []string{"openai"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cacheRaw, _ := json.Marshal(ParsedEntries{
		Domains:  []string{"source.example.com"},
		Keywords: []string{"chatgpt"},
	})
	sealedCache, err := service.Repository.Vault.Seal(cacheRaw)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := service.Repository.Database.ExecContext(ctx,
		`INSERT INTO service_set_sources(id, service_id, url, format, behavior, interval_seconds, position, last_status, last_error_code, last_fetch_at, last_success_at, entry_count, skipped_count, encrypted_cache, created_at, updated_at)
		 VALUES('src-res', ?, 'https://example.com/rules.txt', 'text', 'classical', 86400, 0, 'ok', '', ?, ?, 2, 0, ?, ?, ?)`,
		svc.ID, now, now, sealedCache, now, now); err != nil {
		t.Fatal(err)
	}

	rule := desiredstate.Policy{
		ID:       "pol-1",
		Name:     "Policy with Svc",
		Priority: 10,
		Enabled:  true,
		Action:   desiredstate.Action{Type: "Direct"},
		Match:    desiredstate.Match{ServiceIDs: []string{svc.ID}},
	}

	resolved, err := service.resolve(ctx, rule)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if len(resolved.Match.Keywords) != 2 {
		t.Fatalf("expected 2 keywords, got: %v", resolved.Match.Keywords)
	}
	if len(resolved.Match.Domains) != 1 || resolved.Match.Domains[0] != "source.example.com" {
		t.Fatalf("expected domain from source, got: %v", resolved.Match.Domains)
	}
}

func TestDisabledServiceSetSkipsSources(t *testing.T) {
	service, _, _, _, _ := fixture(t)
	ctx := context.Background()

	svc, err := service.Repository.SaveService(ctx, "", ServiceInput{
		Name:     "Disabled Svc",
		Enabled:  false,
		Keywords: []string{"openai"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cacheRaw, _ := json.Marshal(ParsedEntries{
		Domains: []string{"source.example.com"},
	})
	sealedCache, err := service.Repository.Vault.Seal(cacheRaw)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := service.Repository.Database.ExecContext(ctx,
		`INSERT INTO service_set_sources(id, service_id, url, format, behavior, interval_seconds, position, last_status, last_error_code, last_fetch_at, last_success_at, entry_count, skipped_count, encrypted_cache, created_at, updated_at)
		 VALUES('src-dis', ?, 'https://example.com/rules.txt', 'text', 'classical', 86400, 0, 'ok', '', ?, ?, 1, 0, ?, ?, ?)`,
		svc.ID, now, now, sealedCache, now, now); err != nil {
		t.Fatal(err)
	}

	rule := desiredstate.Policy{
		ID:       "pol-dis",
		Name:     "Policy with Disabled Svc",
		Priority: 10,
		Enabled:  true,
		Action:   desiredstate.Action{Type: "Direct"},
		Match:    desiredstate.Match{ServiceIDs: []string{svc.ID}},
	}

	resolved, err := service.resolve(ctx, rule)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.Enabled {
		t.Errorf("expected policy to be disabled when referencing only disabled service set")
	}
	if len(resolved.Match.Domains) != 0 || len(resolved.Match.Keywords) != 0 {
		t.Errorf("expected no matchers from disabled service set, got domains=%v keywords=%v", resolved.Match.Domains, resolved.Match.Keywords)
	}
}

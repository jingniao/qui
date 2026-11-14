package xray

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/rulesets"
	"proxycontrol/pkg/contracts/desiredstate"
)

const (
	testGeositeURL = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat"
	testGeoipURL   = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat"
)

func ruleSetPolicy(sets ...desiredstate.RuleSet) desiredstate.Policy {
	return desiredstate.Policy{ID: "block-cn", Name: "block cn", Priority: 1, Enabled: true, Match: desiredstate.Match{RuleSets: sets}, Action: desiredstate.Action{Type: "Block"}}
}

func testRuleSets() (desiredstate.RuleSet, desiredstate.RuleSet) {
	domain := desiredstate.RuleSet{ID: "geosite-cn", Behavior: "domain", SingBoxURL: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs", XrayURL: testGeositeURL, XrayTag: "cn", RefreshSeconds: 86400}
	address := desiredstate.RuleSet{ID: "geoip-cn", Behavior: "ipcidr", SingBoxURL: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs", XrayURL: testGeoipURL, XrayTag: "cn", RefreshSeconds: 86400}
	return domain, address
}

func TestXrayRuleSetsRenderAsExtEntriesInDomainAndIPLists(t *testing.T) {
	domain, address := testRuleSets()
	state := testState(1080, 1)
	state.Policies = []desiredstate.Policy{ruleSetPolicy(domain, address)}
	rules, err := compilePolicies(state, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	wantDomain := "ext:" + rulesets.FileName(testGeositeURL, false) + ":cn"
	wantIP := "ext:" + rulesets.FileName(testGeoipURL, false) + ":cn"
	var domainRule, ipRule bool
	for _, rule := range rules {
		if rule.RuleTag != "block-cn" {
			continue
		}
		if len(rule.Domain) == 1 && rule.Domain[0] == wantDomain && len(rule.IP) == 0 {
			domainRule = true
		}
		if len(rule.IP) == 1 && rule.IP[0] == wantIP && len(rule.Domain) == 0 {
			ipRule = true
		}
		if len(rule.Domain) == 0 && len(rule.IP) == 0 {
			t.Fatalf("rule set policy rendered without an address field, which would match everything: %+v", rule)
		}
	}
	if !domainRule || !ipRule {
		t.Fatalf("domain and IP conditions must be separate OR-ed rules: %+v", rules)
	}
}

func TestXrayRuleSetOnlyPolicyAlwaysHasAddressFields(t *testing.T) {
	domain, _ := testRuleSets()
	state := testState(1080, 1)
	state.Policies = []desiredstate.Policy{ruleSetPolicy(domain)}
	rendered, err := Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Routing struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(rendered.Data, &document); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rule := range document.Routing.Rules {
		if rule["outboundTag"] == "block" {
			found = true
			domains, _ := rule["domain"].([]any)
			if len(domains) != 1 || !strings.HasPrefix(domains[0].(string), "ext:") || rule["ip"] != nil {
				t.Fatalf("block rule without its rule set address field: %v", rule)
			}
		}
	}
	if !found {
		t.Fatal("rule set policy was not rendered")
	}
}

func TestXrayRuleSetsAreAddedToLiteralEntries(t *testing.T) {
	domain, address := testRuleSets()
	state := testState(1080, 1)
	policy := ruleSetPolicy(domain, address)
	policy.Match.Suffixes = []string{"example.com"}
	policy.Match.CIDRs = []string{"10.0.0.0/8"}
	state.Policies = []desiredstate.Policy{policy}
	rules, err := compilePolicies(state, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range rules {
		if rule.RuleTag != "block-cn" {
			continue
		}
		if len(rule.Domain) > 0 && (len(rule.Domain) != 2 || rule.Domain[0] != "domain:example.com") {
			t.Fatalf("domain list: %v", rule.Domain)
		}
		if len(rule.IP) > 0 && (len(rule.IP) != 2 || rule.IP[0] != "10.0.0.0/8") {
			t.Fatalf("ip list: %v", rule.IP)
		}
	}
}

func TestXrayAssetEnvironmentPointsAtTheRuleSetDirectory(t *testing.T) {
	directory := t.TempDir()
	environment := rulesets.AssetEnvironment([]string{"A=1", "XRAY_LOCATION_ASSET=/elsewhere"}, directory)
	absolute, _ := filepath.Abs(filepath.Join(directory, "rulesets"))
	count := 0
	for _, entry := range environment {
		if strings.HasPrefix(entry, "XRAY_LOCATION_ASSET=") {
			count++
			if entry != "XRAY_LOCATION_ASSET="+absolute {
				t.Fatalf("environment entry %q", entry)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one asset entry in %v", environment)
	}
}

func TestXrayReportsNativeRuleSets(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("XRAY_BINARY is required to detect capabilities")
	}
	capabilities, err := Detect(context.Background(), os.Getenv("XRAY_BINARY"))
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.OutboundSourceSupported {
		t.Fatal("outbound source unsupported")
	}
	if !capabilities.NativeRuleSetsSupported {
		t.Fatal("native rule sets not reported")
	}
}

// TestRealXrayNativeCheckAcceptsExtRuleSets renders a policy that uses a
// geosite file and runs the core's own configuration test with the rulesets
// directory as its asset location. The geosite.dat (it must have a "cn" list)
// is QUI_GEOSITE_DAT, or by default the one the official Xray release ships next
// to XRAY_BINARY; it is copied under the name the renderer derives from the URL.
func TestRealXrayNativeCheckAcceptsExtRuleSets(t *testing.T) {
	binary, geosite := os.Getenv("XRAY_BINARY"), os.Getenv("QUI_GEOSITE_DAT")
	if binary == "" {
		t.Skip("XRAY_BINARY is required")
	}
	if geosite == "" {
		geosite = filepath.Join(filepath.Dir(binary), "geosite.dat")
	}
	data, err := os.ReadFile(geosite)
	if err != nil {
		t.Fatal(err)
	}
	domain, _ := testRuleSets()
	runtime := t.TempDir()
	if err := os.MkdirAll(rulesets.Dir(runtime), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesets.Dir(runtime), rulesets.FileName(domain.XrayURL, false)), data, 0600); err != nil {
		t.Fatal(err)
	}
	state := testState(availablePort(t), 1)
	state.Policies = []desiredstate.Policy{ruleSetPolicy(domain)}
	adapter := &Adapter{Path: binary, Directory: runtime, NodeID: testNodeID}
	rendered, err := adapter.Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Validate(context.Background(), rendered); err != nil {
		t.Fatal("native check rejected the ext: rule set:", err)
	}
	// Without the file the same configuration must fail the native check.
	os.Remove(filepath.Join(rulesets.Dir(runtime), rulesets.FileName(domain.XrayURL, false)))
	if err := adapter.Validate(context.Background(), rendered); err == nil {
		t.Fatal("native check accepted a missing rule set file")
	}
}

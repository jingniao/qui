package singbox

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/rulesets"
	"proxycontrol/pkg/contracts/desiredstate"
)

func singBoxRuleSet(id, kind string) desiredstate.RuleSet {
	set := desiredstate.RuleSet{ID: id, Behavior: "domain", SingBoxURL: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs", XrayURL: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat", XrayTag: "cn", RefreshSeconds: 86400}
	if kind == "ip" {
		set.Behavior = "ipcidr"
		set.SingBoxURL = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs"
		set.XrayURL = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat"
	}
	return set
}

func ruleSetState(t *testing.T, sets ...desiredstate.RuleSet) desiredstate.State {
	t.Helper()
	state := stateFixture(t, "http", 1080)
	state.Policies = []desiredstate.Policy{{ID: "block-cn", Name: "block cn", Enabled: true, Priority: 10, Match: desiredstate.Match{RuleSets: sets}, Action: desiredstate.Action{Type: "Block"}}}
	return state
}

func renderRuleSets(t *testing.T, state desiredstate.State, directory string) map[string]any {
	t.Helper()
	rendered, err := renderWithOptions(context.Background(), state, time.Now(), renderOptions{ruleSetDirectory: directory})
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(rendered.Data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestSingboxRendersLocalBinaryRuleSetsAndReferencesThem(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "rulesets")
	domain, address := singBoxRuleSet("geosite-cn", "domain"), singBoxRuleSet("geoip-cn", "ip")
	document := renderRuleSets(t, ruleSetState(t, domain, address, domain), directory)
	route := document["route"].(map[string]any)
	declared := route["rule_set"].([]any)
	if len(declared) != 2 {
		t.Fatalf("each tag must be declared once: %v", declared)
	}
	first := declared[0].(map[string]any)
	if first["type"] != "local" || first["format"] != "binary" || first["tag"] != "qui-rs-geosite-cn" || first["path"] != filepath.Join(directory, rulesets.FileName(domain.SingBoxURL, true)) {
		t.Fatalf("rule_set entry: %v", first)
	}
	if !strings.HasSuffix(first["path"].(string), ".srs") {
		t.Fatalf("path %v", first["path"])
	}
	found := false
	for _, entry := range route["rules"].([]any) {
		rule := entry.(map[string]any)
		references, ok := rule["rule_set"].([]any)
		if !ok {
			continue
		}
		found = true
		if rule["action"] != "reject" || len(references) != 3 || references[0] != "qui-rs-geosite-cn" || references[1] != "qui-rs-geoip-cn" {
			t.Fatalf("rule: %v", rule)
		}
	}
	if !found {
		t.Fatal("no route rule references the rule sets")
	}
}

func TestSingboxRuleSetOnlyPolicyNeverRendersWithoutAddressFields(t *testing.T) {
	domain := singBoxRuleSet("geosite-cn", "domain")
	document := renderRuleSets(t, ruleSetState(t, domain), t.TempDir())
	for _, entry := range document["route"].(map[string]any)["rules"].([]any) {
		rule := entry.(map[string]any)
		if rule["action"] == "reject" && rule["inbound"] == nil && rule["port"] == nil && rule["rule_set"] == nil && rule["auth_user"] == nil {
			t.Fatalf("reject rule without any condition would block everything: %v", rule)
		}
	}
	if _, err := renderWithOptions(context.Background(), ruleSetState(t, domain), time.Now(), renderOptions{}); err == nil {
		t.Fatal("rule sets without a runtime directory must not render")
	}
}

func TestSingboxReportsNativeRuleSets(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("SINGBOX_BINARY is required to detect capabilities")
	}
	capabilities, err := Detect(context.Background(), binary)
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

// TestRealSingboxNativeCheckAcceptsLocalRuleSets compiles a tiny JSON rule set
// into a binary .srs, renders a policy that references it and runs sing-box's
// own check.
func TestRealSingboxNativeCheckAcceptsLocalRuleSets(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("SINGBOX_BINARY is required")
	}
	runtime := t.TempDir()
	if err := os.MkdirAll(rulesets.Dir(runtime), 0700); err != nil {
		t.Fatal(err)
	}
	domain := singBoxRuleSet("geosite-cn", "domain")
	source := filepath.Join(t.TempDir(), "cn.json")
	if err := os.WriteFile(source, []byte(`{"version":3,"rules":[{"domain_suffix":["example.cn"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(rulesets.Dir(runtime), rulesets.FileName(domain.SingBoxURL, true))
	if output, err := exec.Command(binary, "rule-set", "compile", "--output", target, source).CombinedOutput(); err != nil {
		t.Fatalf("rule-set compile: %v: %s", err, output)
	}
	adapter := &Adapter{Path: binary, Directory: runtime, NodeID: "test-node"}
	rendered, err := adapter.Render(context.Background(), ruleSetState(t, domain))
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Validate(context.Background(), rendered); err != nil {
		t.Fatal("native check rejected the local rule set:", err)
	}
	os.Remove(target)
	if err := adapter.Validate(context.Background(), rendered); err == nil {
		t.Fatal("native check accepted a missing rule set file")
	}
}

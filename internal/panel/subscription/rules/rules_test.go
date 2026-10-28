package rules

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/platform/db"
)

// fakeLookup knows one service rule, one node group and one node.
type fakeLookup struct{}

func (fakeLookup) ServiceSetExists(_ context.Context, id string) bool { return id == "svc-1" }
func (fakeLookup) NodeGroupExists(_ context.Context, id string) bool  { return id == "group-1" }
func (fakeLookup) NodeExists(_ context.Context, id string) bool       { return id == "node-1" }

func TestPresetsAreValid(t *testing.T) {
	for _, preset := range Presets() {
		if err := Validate(context.Background(), preset.Template, fakeLookup{}); err != nil {
			t.Fatalf("preset %s: %v", preset.ID, err)
		}
	}
	if Default().Preset != "standard" {
		t.Fatal("the default template is not the recommended preset")
	}
}

// A2.1: every rule of plan section 4.4 is enforced, and the failure names the
// field it belongs to.
func TestValidationCoversEveryRule(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		change func(*Template)
		field  string
	}{
		{"mode", func(t *Template) { t.Client.Mode = "auto" }, "client.mode"},
		{"mixed port", func(t *Template) { t.Client.MixedPort = 80 }, "client.mixedPort"},
		{"log level", func(t *Template) { t.Client.LogLevel = "trace" }, "client.logLevel"},
		{"dns mode", func(t *Template) { t.DNS = DNS{Enable: true, EnhancedMode: "magic", Nameservers: []string{"1.1.1.1"}} }, "dns.enhancedMode"},
		{"dns without servers", func(t *Template) { t.DNS = DNS{Enable: true, EnhancedMode: "fake-ip"} }, "dns.nameservers"},
		{"dns server", func(t *Template) {
			t.DNS = DNS{Enable: true, EnhancedMode: "fake-ip", Nameservers: []string{"ftp://dns.example"}}
		}, "dns.nameservers"},
		{"no groups", func(t *Template) { t.Groups = nil }, "groups"},
		{"too many groups", func(t *Template) {
			for index := 0; index < MaxGroups; index++ {
				t.Groups = append(t.Groups, Group{Name: "G" + strings.Repeat("x", index%5) + string(rune('a'+index%26)) + string(rune('a'+index/26)), Type: "select", Members: []string{TargetDirect}})
			}
		}, "groups"},
		{"duplicate group", func(t *Template) { t.Groups[1].Name = "PROXY" }, "groups[1].name"},
		{"reserved group name", func(t *Template) { t.Groups[3].Name = "GLOBAL" }, "groups[3].name"},
		{"comma in group name", func(t *Template) { t.Groups[3].Name = "A,B" }, "groups[3].name"},
		{"group type", func(t *Template) { t.Groups[0].Type = "load-balance" }, "groups[0].type"},
		{"empty group", func(t *Template) { t.Groups[4].Members = nil }, "groups[4].members"},
		{"unknown member", func(t *Template) { t.Groups[4].Members = []string{"NOPE"} }, "groups[4].members"},
		{"empty source member", func(t *Template) { t.Groups[4].Members = []string{"@source:"} }, "groups[4].members"},
		{"unknown node group", func(t *Template) { t.Groups[4].Members = []string{"@nodeGroup:missing"} }, "groups[4].members"},
		{"self member", func(t *Template) { t.Groups[4].Members = []string{"ADBLOCK"} }, "groups[4].members"},
		{"group cycle", func(t *Template) { t.Groups[1].Members = []string{"PROXY"} }, "groups"},
		{"test url", func(t *Template) { t.Groups[1].TestURL = "ftp://x" }, "groups[1].testUrl"},
		{"test interval", func(t *Template) { t.Groups[1].IntervalSeconds = 1 }, "groups[1].intervalSeconds"},
		{"provider not https", func(t *Template) {
			t.Providers = []Provider{{Name: "ads", Behavior: "domain", URL: "http://rules.example/ads.txt", IntervalSeconds: 86400}}
		}, "providers[0].url"},
		{"provider behavior", func(t *Template) {
			t.Providers = []Provider{{Name: "ads", Behavior: "mixed", URL: "https://rules.example/ads.txt", IntervalSeconds: 86400}}
		}, "providers[0].behavior"},
		{"too many providers", func(t *Template) {
			for index := 0; index <= MaxProviders; index++ {
				t.Providers = append(t.Providers, Provider{Name: "p" + string(rune('a'+index%26)) + string(rune('a'+index/26)), Behavior: "domain", URL: "https://rules.example/x", IntervalSeconds: 86400})
			}
		}, "providers"},
		{"too many rules", func(t *Template) {
			for index := 0; index <= MaxRules; index++ {
				t.Rules = append(t.Rules, Rule{ID: "x", Type: RuleDomain, Payload: "a.example", Target: TargetDirect, Enabled: true, Priority: 5})
			}
		}, "rules"},
		{"duplicate rule id", func(t *Template) { t.Rules[1].ID = t.Rules[0].ID }, "rules[1].id"},
		{"domain syntax", func(t *Template) { t.Rules[3].Type, t.Rules[3].Payload = RuleDomainSuffix, "bad domain" }, "rules[3].payload"},
		{"keyword comma", func(t *Template) { t.Rules[3].Payload = "a,b" }, "rules[3].payload"},
		{"cidr syntax", func(t *Template) { t.Rules[0].Payload = "127.0.0.1/8" }, "rules[0].payload"},
		{"cidr6 family", func(t *Template) { t.Rules[0].Type = RuleIPCIDR6 }, "rules[0].payload"},
		{"geoip", func(t *Template) { t.Rules[6].Payload = "CHN" }, "rules[6].payload"},
		{"geosite", func(t *Template) { t.Rules[6].Type, t.Rules[6].Payload = RuleGeoSite, "CN Sites" }, "rules[6].payload"},
		{"port comma", func(t *Template) { t.Rules[3].Type, t.Rules[3].Payload = RuleDstPort, "80,443" }, "rules[3].payload"},
		{"port range", func(t *Template) { t.Rules[3].Type, t.Rules[3].Payload = RuleDstPort, "443-80" }, "rules[3].payload"},
		{"unknown provider", func(t *Template) { t.Rules[3].Type, t.Rules[3].Payload = RuleSet, "ads" }, "rules[3].payload"},
		{"unknown service", func(t *Template) { t.Rules[3].Type, t.Rules[3].Payload = RuleSet, "service:svc-2" }, "rules[3].payload"},
		{"unknown type", func(t *Template) { t.Rules[3].Type = "PROCESS-NAME" }, "rules[3].payload"},
		{"no-resolve on domain", func(t *Template) { t.Rules[3].NoResolve = true }, "rules[3].noResolve"},
		{"unknown target", func(t *Template) { t.Rules[3].Target = "NOPE" }, "rules[3].target"},
		{"unknown node target", func(t *Template) { t.Rules[3].Target = "node:node-2" }, "rules[3].target"},
		{"two matches", func(t *Template) { t.Rules[3].Type, t.Rules[3].Payload = RuleMatch, "" }, "rules"},
		{"match not last", func(t *Template) { t.Rules[7].Priority = 5 }, "rules"},
		{"no enabled match", func(t *Template) { t.Rules[7].Enabled = false }, "rules"},
		{"payload on match", func(t *Template) { t.Rules[7].Payload = "x" }, "rules[7].payload"},
		{"priority", func(t *Template) { t.Rules[0].Priority = 0 }, "rules[0].priority"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			template := Default()
			testCase.change(&template)
			err := Validate(context.Background(), template, fakeLookup{})
			var field *FieldError
			if !errors.As(err, &field) || field.Field != testCase.field {
				t.Fatalf("error %v, want a failure of %s", err, testCase.field)
			}
		})
	}
}

func TestValidationAcceptsEveryType(t *testing.T) {
	template := Default()
	template.Providers = []Provider{{Name: "ads", Behavior: "domain", URL: "https://rules.example/ads.txt", IntervalSeconds: 86400}}
	template.DNS = DNS{Enable: true, EnhancedMode: "fake-ip", Nameservers: []string{"223.5.5.5", "https://dns.example/dns-query", "tls://1.1.1.1"}, Fallback: []string{"8.8.8.8"}}
	template.Groups = append(template.Groups, Group{Name: "日本", Type: "select", Members: []string{"@nodeGroup:group-1", TargetDirect}})
	extra := []Rule{
		{ID: "a", Type: RuleDomain, Payload: "api.example.com", Target: "日本", Enabled: true, Priority: 20},
		{ID: "b", Type: RuleIPCIDR6, Payload: "2001:db8::/32", Target: TargetDirect, NoResolve: true, Enabled: true, Priority: 21},
		{ID: "c", Type: RuleGeoSite, Payload: "category-ads-all", Target: TargetReject, Enabled: true, Priority: 22},
		{ID: "d", Type: RuleDstPort, Payload: "80/443/8000-8100", Target: "PROXY", Enabled: true, Priority: 23},
		{ID: "e", Type: RuleSet, Payload: "ads", Target: TargetReject, Enabled: true, Priority: 24},
		{ID: "f", Type: RuleSet, Payload: "service:svc-1", Target: "node:node-1", Enabled: true, Priority: 25},
		{ID: "g", Type: RuleGeoIP, Payload: "LAN", Target: TargetDirect, NoResolve: true, Enabled: true, Priority: 26},
		{ID: "h", Type: RuleDomain, Payload: "disabled match is ignored", Target: TargetDirect, Enabled: false, Priority: 1000},
	}
	template.Rules = append(template.Rules, extra...)
	template.Rules[len(template.Rules)-1].Type, template.Rules[len(template.Rules)-1].Payload = RuleMatch, ""
	if err := Validate(context.Background(), template, fakeLookup{}); err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return &Store{Database: database, Lookup: fakeLookup{}}
}

// A2.2: a save built on a stale revision is refused, an unchanged template is
// not a new revision, and an invalid template is never stored.
func TestStoreRevisions(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	initial, err := store.Read(ctx)
	if err != nil || initial.Revision != 0 || initial.Template.Preset != "standard" {
		t.Fatalf("initial template %+v %v", initial, err)
	}
	template := Presets()[1].Template
	saved, changed, err := store.Save(ctx, template, 0, "owner")
	if err != nil || !changed || saved.Revision != 1 {
		t.Fatalf("first save %+v %t %v", saved, changed, err)
	}
	if _, changed, err := store.Save(ctx, template, 1, "owner"); err != nil || changed {
		t.Fatalf("unchanged save %t %v", changed, err)
	}
	if _, _, err := store.Save(ctx, Presets()[2].Template, 0, "other"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	broken := Presets()[2].Template
	broken.Rules = nil
	if _, _, err := store.Save(ctx, broken, 1, "owner"); err == nil {
		t.Fatal("an invalid template was stored")
	}
	read, err := store.Read(ctx)
	if err != nil || read.Revision != 1 || read.Template.Preset != "minimal" || read.UpdatedBy != "owner" {
		t.Fatalf("stored template %+v %v", read, err)
	}
	if _, err := store.Database.ExecContext(ctx, "UPDATE system_settings SET value_json=replace(value_json,'\"preset\"','\"unknownField\"') WHERE key='client_rules'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx); !errors.Is(err, ErrStored) {
		t.Fatalf("a row with an unknown field was accepted: %v", err)
	}
}

func endpoint(name, nodeID string, groups ...string) proxy.Proxy {
	labels := []string{}
	for _, group := range groups {
		labels = append(labels, "nodeGroup:"+group)
	}
	return proxy.Proxy{Name: name, Origin: proxy.Origin{Source: "published", Node: proxy.Ref{ID: nodeID}, Labels: labels}}
}

// A2.4: @nodeGroup expands to the user's own proxies of that granted group only;
// an empty expansion becomes DIRECT with a warning; references the user cannot
// use are dropped with a warning, never rewritten.
func TestExpandPerUser(t *testing.T) {
	template := Default()
	template.Groups = append(template.Groups, Group{Name: "JP", Type: "select", Members: []string{"@nodeGroup:jp"}}, Group{Name: "US", Type: "select", Members: []string{"@nodeGroup:us"}})
	template.Rules = append([]Rule{
		{ID: "jp", Type: RuleDomainSuffix, Payload: "jp.example", Target: "JP", Enabled: true, Priority: 4},
		{ID: "svc", Type: RuleSet, Payload: "service:svc-1", Target: "PROXY", Enabled: true, Priority: 5},
		{ID: "svc-again", Type: RuleSet, Payload: "service:svc-1", Target: TargetReject, Enabled: true, Priority: 6},
		{ID: "node", Type: RuleDomain, Payload: "n.example", Target: "node:node-9", Enabled: true, Priority: 7},
		{ID: "off", Type: RuleDomain, Payload: "off.example", Target: TargetDirect, Enabled: false, Priority: 8},
	}, template.Rules...)
	proxies := []proxy.Proxy{endpoint("Tokyo · vless", "node-1", "jp"), endpoint("Osaka · trojan", "node-2", "jp"), endpoint("Seoul · tuic", "node-3")}
	resolved := Expand(template, proxies)
	members := map[string][]string{}
	for _, group := range resolved.Groups {
		for _, member := range group.Members {
			members[group.Name] = append(members[group.Name], member.Name)
		}
	}
	for _, member := range resolved.Groups[0].Members {
		if want := map[string]string{"AUTO": KindGroup, "FALLBACK": KindGroup, "DIRECT": KindBuiltin}[member.Name]; want != "" && member.Kind != want {
			t.Fatalf("member %+v has the wrong kind", member)
		}
	}
	if strings.Join(members["JP"], "|") != "Tokyo · vless|Osaka · trojan" {
		t.Fatalf("JP members %v", members["JP"])
	}
	if strings.Join(members["US"], "|") != "DIRECT" {
		t.Fatalf("an ungranted group expanded to %v", members["US"])
	}
	if strings.Join(members["AUTO"], "|") != "Tokyo · vless|Osaka · trojan|Seoul · tuic" {
		t.Fatalf("@all expanded to %v", members["AUTO"])
	}
	if strings.Join(members["PROXY"], "|") != "AUTO|FALLBACK|Tokyo · vless|Osaka · trojan|Seoul · tuic|DIRECT" {
		t.Fatalf("PROXY members %v", members["PROXY"])
	}
	types := []string{}
	for _, rule := range resolved.Rules {
		types = append(types, rule.Type+":"+rule.Payload+"→"+rule.Target.Name)
	}
	got := strings.Join(types, " ")
	if !strings.HasPrefix(got, "IP-CIDR:127.0.0.0/8→DIRECT") || !strings.HasSuffix(got, "MATCH:→PROXY") {
		t.Fatalf("rules out of order: %s", got)
	}
	if strings.Contains(got, "n.example") || strings.Contains(got, "off.example") {
		t.Fatalf("an unusable or disabled rule survived: %s", got)
	}
	if !strings.Contains(got, "RULE-SET:qui-service-svc-1→PROXY") {
		t.Fatalf("service rule set not mapped to its provider: %s", got)
	}
	if len(resolved.Providers) != 1 || resolved.Providers[0].ServiceSetID != "svc-1" || resolved.Providers[0].Name != "qui-service-svc-1" {
		t.Fatalf("providers %+v", resolved.Providers)
	}
	if len(resolved.Warnings) != 2 {
		t.Fatalf("warnings %+v", resolved.Warnings)
	}
}

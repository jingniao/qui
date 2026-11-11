package rules

import (
	"context"
	"strings"
	"testing"
)

func TestPresetsValidate(t *testing.T) {
	seen := map[string]bool{}
	for _, preset := range Presets() {
		if seen[preset.ID] {
			t.Fatalf("duplicate preset id %q", preset.ID)
		}
		seen[preset.ID] = true
		if preset.Template.Preset != preset.ID {
			t.Fatalf("%s: template preset label %q", preset.ID, preset.Template.Preset)
		}
		if err := Validate(context.Background(), preset.Template, nil); err != nil {
			t.Fatalf("%s: %v", preset.ID, err)
		}
	}
}

// The 屏蔽回国 presets REJECT Chinese domains and IPs, never send them DIRECT
// or through a group a client could switch back to DIRECT, and only use
// providers the template declares.
func TestBlockChinaPresets(t *testing.T) {
	want := map[string]int{"meta-lite": 3, "meta-standard": 8, "meta-full": 23}
	for _, preset := range Presets() {
		services, ok := want[preset.ID]
		if !ok {
			continue
		}
		delete(want, preset.ID)
		template := preset.Template
		providers := map[string]Provider{}
		for _, provider := range template.Providers {
			providers[provider.Name] = provider
			if provider.Format != "mrs" || !strings.HasPrefix(provider.URL, metaRulesBase) {
				t.Fatalf("%s: provider %+v", preset.ID, provider)
			}
		}
		if len(template.Groups) != services+2 {
			t.Fatalf("%s: %d groups, want %d", preset.ID, len(template.Groups), services+2)
		}
		blocked := map[string]bool{}
		used := map[string]bool{}
		for _, rule := range Ordered(template.Rules) {
			if rule.Type != RuleSet {
				continue
			}
			used[rule.Payload] = true
			if _, ok := providers[rule.Payload]; !ok {
				t.Fatalf("%s: rule %s uses undeclared provider %q", preset.ID, rule.ID, rule.Payload)
			}
			switch rule.Payload {
			case "geolocation-cn", "cn-domains", "cn":
				if rule.Target != TargetReject {
					t.Fatalf("%s: %s goes to %q", preset.ID, rule.Payload, rule.Target)
				}
				blocked[rule.Payload] = true
			}
		}
		if len(blocked) != 3 {
			t.Fatalf("%s: blocked sets %v", preset.ID, blocked)
		}
		if len(used) != len(providers) {
			t.Fatalf("%s: %d providers declared, %d used", preset.ID, len(providers), len(used))
		}
		if providers["cn"].Behavior != "ipcidr" || providers["cn-domains"].URL != metaRulesBase+"geosite/cn.mrs" {
			t.Fatalf("%s: cn providers %+v %+v", preset.ID, providers["cn"], providers["cn-domains"])
		}
		// Chinese domains are rejected before any service set can claim them.
		ordered := Ordered(template.Rules)
		if ordered[len(ordered)-1].Type != RuleMatch {
			t.Fatalf("%s: last rule %+v", preset.ID, ordered[len(ordered)-1])
		}
		for index, rule := range ordered {
			if rule.Payload == "geolocation-cn" {
				for _, earlier := range ordered[:index] {
					if earlier.Target != TargetReject && earlier.Payload != "private" && earlier.Target != "🛑 广告拦截" {
						t.Fatalf("%s: %s runs before the China block", preset.ID, earlier.Payload)
					}
				}
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing presets %v", want)
	}
}

package desiredstate

import (
	"errors"
	"testing"
)

const (
	testSRS = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs"
	testDat = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat"
)

func TestRuleSetURLAllowed(t *testing.T) {
	for address, want := range map[string]bool{
		testSRS: true,
		"https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs":                       true,
		"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/category-ads-all.srs": true,
		"http://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs":                false,
		"https://example.com/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs":                          false,
		"https://testingcf.jsdelivr.net:8443/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs":          false,
		testSRS + "?x=1": false,
		testSRS + "#x":   false,
		"https://user@testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs": false,
		"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/../cn.srs":   false,
		"https://testingcf.jsdelivr.net/gh/Other/meta-rules-dat@sing/geo/geosite/cn.srs":          false,
		"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/cn.yaml":     false,
		testDat: false, // not a .srs
	} {
		if got := RuleSetURLAllowed(address, true); got != want {
			t.Errorf("sing-box %q: %v, want %v", address, got, want)
		}
	}
	for address, want := range map[string]bool{
		testDat: true,
		"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat":      true,
		"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip-lite.dat": false,
		"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geosite.dat":       false,
		testSRS: false,
	} {
		if got := RuleSetURLAllowed(address, false); got != want {
			t.Errorf("xray %q: %v, want %v", address, got, want)
		}
	}
}

func TestValidateRuleSet(t *testing.T) {
	good := RuleSet{ID: "builtin-service-cn-native-1", Behavior: "domain", SingBoxURL: testSRS, XrayURL: testDat, XrayTag: "cn", RefreshSeconds: 86400}
	if err := ValidateRuleSet(good); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*RuleSet){
		"behavior":      func(s *RuleSet) { s.Behavior = "classical" },
		"ip behavior":   func(s *RuleSet) { s.Behavior = "ipcidr" }, // files are geosite
		"tag":           func(s *RuleSet) { s.XrayTag = "CN" },
		"id":            func(s *RuleSet) { s.ID = "" },
		"refresh":       func(s *RuleSet) { s.RefreshSeconds = 60 },
		"sing-box host": func(s *RuleSet) { s.SingBoxURL = "https://example.com/x.srs" },
		"xray url":      func(s *RuleSet) { s.XrayURL = testSRS },
	} {
		set := good
		change(&set)
		if ValidateRuleSet(set) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestRequireNativeRuleSetSupport(t *testing.T) {
	state := State{Policies: []Policy{{Enabled: true, Match: Match{RuleSets: []RuleSet{{ID: "x"}}}}}}
	supported := []CoreKeywordCapability{{Name: CoreXray, NativeRuleSetsSupported: true}, {Name: CoreGost}}
	if err := RequireNativeRuleSetSupport(state, supported); err != nil {
		t.Fatal(err)
	}
	old := append(supported, CoreKeywordCapability{Name: CoreSingBox})
	if err := RequireNativeRuleSetSupport(state, old); !errors.Is(err, ErrNativeRuleSetUnsupported) {
		t.Fatal("old core accepted:", err)
	}
	state.Policies[0].Enabled = false
	if err := RequireNativeRuleSetSupport(state, old); err != nil {
		t.Fatal("disabled policy refused:", err)
	}
}

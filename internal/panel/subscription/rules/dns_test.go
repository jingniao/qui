package rules

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDNSExtrasValidation(t *testing.T) {
	if err := validateDNS(RecommendedDNS()); err != nil {
		t.Fatalf("recommended DNS rejected: %v", err)
	}
	for name, dns := range map[string]DNS{
		"default nameserver not an IP": {DefaultNameservers: []string{"dns.alidns.com"}},
		"proxy nameserver scheme":      {ProxyServerNameservers: []string{"ftp://1.1.1.1"}},
		"fake-ip filter with space":    {FakeIPFilter: []string{"bad name"}},
		"geoip without code":           {FallbackFilter: &FallbackFilter{GeoIP: true}},
		"code without geoip":           {FallbackFilter: &FallbackFilter{GeoIPCode: "CN"}},
		"lowercase code":               {FallbackFilter: &FallbackFilter{GeoIP: true, GeoIPCode: "cn"}},
		"bad ipcidr":                   {FallbackFilter: &FallbackFilter{IPCIDR: []string{"240.0.0.0"}}},
		"bad filter domain":            {FallbackFilter: &FallbackFilter{Domain: []string{"+.goo gle.com"}}},
	} {
		if err := validateDNS(dns); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	valid := DNS{Enable: true, EnhancedMode: "fake-ip", Nameservers: []string{"223.5.5.5"}, Fallback: []string{"https://dns.google/dns-query"},
		FallbackFilter: &FallbackFilter{GeoIP: true, GeoIPCode: "CN", IPCIDR: []string{"240.0.0.0/4"}, Domain: []string{"+.google.com"}}}
	if err := validateDNS(valid); err != nil {
		t.Fatalf("valid filter rejected: %v", err)
	}
}

func TestMetaPresetsUseRecommendedDNSAndWarnOnBareFallback(t *testing.T) {
	for _, preset := range Presets() {
		if !strings.HasPrefix(preset.ID, "meta-") {
			continue
		}
		dns := preset.Template.DNS
		if len(dns.Fallback) != 0 || len(dns.DefaultNameservers) == 0 || len(dns.ProxyServerNameservers) == 0 || len(dns.FakeIPFilter) == 0 {
			t.Fatalf("%s DNS = %+v", preset.ID, dns)
		}
		if DNSFallbackWarning(dns) != "" {
			t.Fatalf("%s warns", preset.ID)
		}
	}
	old := DNS{Enable: true, EnhancedMode: "fake-ip", Nameservers: []string{"https://doh.pub/dns-query"}, Fallback: []string{"https://dns.google/dns-query"}}
	resolved := Expand(Template{DNS: old}, nil)
	found := false
	for _, warning := range resolved.Warnings {
		found = found || strings.Contains(warning.Message, "fallback-filter")
	}
	if !found {
		t.Fatalf("no fallback warning in %+v", resolved.Warnings)
	}
}

func TestOldDNSTemplateKeepsItsJSON(t *testing.T) {
	old := DNS{Enable: true, EnhancedMode: "fake-ip", Nameservers: []string{"223.5.5.5"}, Fallback: []string{}}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"enable":true,"enhancedMode":"fake-ip","nameservers":["223.5.5.5"],"fallback":[]}` {
		t.Fatalf("old DNS JSON changed: %s", data)
	}
}

package clash

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/rules"
)

func TestDNSExtrasByDialect(t *testing.T) {
	dns := rules.RecommendedDNS()
	dns.FallbackFilter = &rules.FallbackFilter{GeoIP: true, GeoIPCode: "CN", IPCIDR: []string{"240.0.0.0/4"}, Domain: []string{"+.google.com"}}
	for _, dialect := range []capabilities{meta, legacy} {
		t.Run(dialect.id, func(t *testing.T) {
			resolved := rules.Expand(rules.Template{DNS: dns}, nil)
			out, err := render(dialect, format.Profile{Rules: &resolved})
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				DNS map[string]any `yaml:"dns"`
			}
			if err := yaml.Unmarshal(out.Body, &doc); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"default-nameserver":      dns.DefaultNameservers,
				"proxy-server-nameserver": dns.ProxyServerNameservers,
				"fake-ip-filter":          dns.FakeIPFilter,
				"fallback-filter":         map[string]any{"geoip": true, "geoip-code": "CN", "ipcidr": dns.FallbackFilter.IPCIDR, "domain": dns.FallbackFilter.Domain},
			}
			for key, value := range want {
				if dialect.id == legacy.id {
					if _, ok := doc.DNS[key]; ok {
						t.Errorf("legacy contains %s", key)
					}
					continue
				}
				encoded, err := yaml.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var normalized any
				if err := yaml.Unmarshal(encoded, &normalized); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(doc.DNS[key], normalized) {
					t.Errorf("%s = %#v, want %#v", key, doc.DNS[key], normalized)
				}
			}
		})
	}
}

func TestOldDNSOmitsExtras(t *testing.T) {
	resolved := rules.Expand(rules.Template{DNS: rules.DNS{Enable: true, EnhancedMode: "fake-ip", Nameservers: []string{"223.5.5.5"}, Fallback: []string{"1.1.1.1"}}}, nil)
	out, err := (Meta{}).Render(format.Profile{Rules: &resolved})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"default-nameserver:", "proxy-server-nameserver:", "fake-ip-filter:", "fallback-filter:"} {
		if strings.Contains(string(out.Body), key) {
			t.Errorf("old DNS contains %s", key)
		}
	}
}

func TestBlockChinaPresetDNSExport(t *testing.T) {
	count := 0
	for _, preset := range rules.Presets() {
		if !strings.HasPrefix(preset.ID, "meta-") {
			continue
		}
		count++
		resolved := rules.Expand(preset.Template, nil)
		out, err := (Meta{}).Render(format.Profile{Rules: &resolved})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out.Body), "fallback:") || !strings.Contains(string(out.Body), "default-nameserver:") {
			t.Errorf("%s DNS: %s", preset.ID, out.Body)
		}
	}
	if count != 3 {
		t.Fatalf("tested %d presets", count)
	}
}

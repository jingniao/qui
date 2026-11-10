package clash_test

import (
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

func TestExternalRuleSourcesExportInClash(t *testing.T) {
	proxies := []proxy.Proxy{
		{
			Origin:   proxy.Origin{Source: "published", Ref: "p1", Node: proxy.Ref{ID: "n1", Name: "Node 1"}},
			Name:     "PROXY",
			Protocol: "socks5",
			Server:   "127.0.0.1",
			Port:     1080,
		},
	}
	template := rules.Template{
		Rules: []rules.Rule{
			{
				Type:    rules.RuleSet,
				Payload: "service:svc-1",
				Target:  rules.TargetDirect,
				Enabled: true,
			},
			{
				Type:    rules.RuleMatch,
				Target:  rules.TargetDirect,
				Enabled: true,
			},
		},
	}
	resolved := rules.Expand(template, proxies)

	profile := format.Profile{
		Proxies: proxies,
		Rules:   &resolved,
		ProviderURL: func(id string) string {
			return "https://panel.example/sub/TOKEN/rules/" + id
		},
		ServiceSetSources: func(id string) []format.ExternalProvider {
			if id == "svc-1" {
				return []format.ExternalProvider{
					{
						URL:             "https://example.com/rules.txt",
						Format:          "text",
						Behavior:        "classical",
						IntervalSeconds: 86400,
					},
					{
						URL:             "https://example.com/rules.mrs",
						Format:          "mrs",
						Behavior:        "domain",
						IntervalSeconds: 3600,
					},
				}
			}
			return nil
		},
	}

	output, err := clash.Meta{}.Render(profile)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	body := string(output.Body)

	// Check providers exist
	if !strings.Contains(body, "qui-service-svc-1:") {
		t.Fatal("missing main provider qui-service-svc-1")
	}
	if !strings.Contains(body, "qui-service-svc-1-src1:") {
		t.Fatal("missing source 1 provider qui-service-svc-1-src1")
	}
	if !strings.Contains(body, "qui-service-svc-1-src2:") {
		t.Fatal("missing source 2 provider qui-service-svc-1-src2")
	}

	// Check mrs format in provider
	if !strings.Contains(body, "format: mrs") {
		t.Fatal("missing format: mrs in rule-providers")
	}

	// Check rules order
	mainRuleIdx := strings.Index(body, "RULE-SET,qui-service-svc-1,DIRECT")
	src1RuleIdx := strings.Index(body, "RULE-SET,qui-service-svc-1-src1,DIRECT")
	src2RuleIdx := strings.Index(body, "RULE-SET,qui-service-svc-1-src2,DIRECT")

	if mainRuleIdx == -1 || src1RuleIdx == -1 || src2RuleIdx == -1 {
		t.Fatalf("missing RULE-SET lines in body:\n%s", body)
	}
	if !(mainRuleIdx < src1RuleIdx && src1RuleIdx < src2RuleIdx) {
		t.Fatalf("RULE-SET lines not in order: main=%d, src1=%d, src2=%d", mainRuleIdx, src1RuleIdx, src2RuleIdx)
	}
}

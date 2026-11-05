package clash

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

func TestEmptyPolicyAfterDialectFiltering(t *testing.T) {
	endpoint := proxy.Proxy{Name: "only vless", Server: "example.com", Port: 443, Protocol: "vless", Credential: proxy.Credential{Password: "00000000-0000-4000-8000-000000000001"}}
	for _, policy := range []string{"", "direct", "reject"} {
		t.Run(policy, func(t *testing.T) {
			template := rules.Default()
			template.Groups = []rules.Group{{Name: "test", Type: "select", Members: []string{rules.MemberAll}, EmptyPolicy: policy}}
			resolved := rules.Expand(template, []proxy.Proxy{endpoint})
			if resolved.Groups[0].Members[0].Kind != rules.KindProxy {
				t.Fatal("fallback happened before format filtering")
			}
			output, err := (Legacy{}).Render(format.Profile{Proxies: []proxy.Proxy{endpoint}, Rules: &resolved})
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Groups []struct {
					Proxies []string `yaml:"proxies"`
				} `yaml:"proxy-groups"`
			}
			if err := yaml.Unmarshal(output.Body, &document); err != nil {
				t.Fatal(err)
			}
			want := rules.TargetDirect
			if policy == "reject" {
				want = rules.TargetReject
			}
			if len(document.Groups[0].Proxies) != 1 || document.Groups[0].Proxies[0] != want {
				t.Fatal(string(output.Body))
			}
			warned := false
			for _, raised := range output.Warnings {
				warned = warned || strings.Contains(raised.Message, "已改为 "+want)
			}
			if !warned {
				t.Fatal("missing fallback warning", output.Warnings)
			}
		})
	}
}

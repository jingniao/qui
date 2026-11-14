package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/proxy"
)

func TestExcludeEndpointsBackupRoundTrip(t *testing.T) {
	template := Default()
	raw, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(raw), `"members":`, `"excludeEndpoints":["@endpoint:inbound:one","@endpoint:import:source:Name|socks5"],"members":`, 1)
	var decoded Template
	if err := Decode([]byte(updated), &decoded); err != nil {
		t.Fatal(err)
	}
	if err := Validate(context.Background(), decoded, nil); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"excludeEndpoints":["@endpoint:inbound:one","@endpoint:import:source:Name|socks5"]`) {
		t.Fatal(string(after))
	}
}

func TestExcludeEndpointsValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		refs  []string
		valid bool
	}{
		{"inbound relay import", []string{"@endpoint:inbound:a", "@endpoint:relay:b", "@endpoint:import:source:Name|socks5"}, true},
		{"missing prefix", []string{"inbound:a"}, false},
		{"dynamic member", []string{MemberAll}, false},
		{"builtin", []string{TargetDirect}, false},
		{"unknown kind", []string{"@endpoint:node:a"}, false},
		{"empty id", []string{"@endpoint:inbound:"}, false},
		{"invalid import", []string{"@endpoint:import:source:Name|"}, false},
		{"duplicate", []string{"@endpoint:inbound:a", "@endpoint:inbound:a"}, false},
		{"limit", endpointExclusions(1000), true},
		{"over limit", endpointExclusions(1001), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			template := Default()
			template.Groups[0].ExcludeEndpoints = tc.refs
			err := Validate(context.Background(), template, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if !tc.valid && !strings.Contains(err.Error(), ".excludeEndpoints") {
				t.Fatalf("wrong field: %v", err)
			}
		})
	}
}

func endpointExclusions(count int) []string {
	refs := make([]string, count)
	for i := range refs {
		refs[i] = fmt.Sprintf("@endpoint:inbound:id%d", i)
	}
	return refs
}

func TestExcludeEndpointsExpansion(t *testing.T) {
	proxies := []proxy.Proxy{
		{Name: "US one", Origin: proxy.Origin{Source: "published", Ref: "one", Labels: []string{"nodeGroup:group"}}},
		{Name: "US two", Origin: proxy.Origin{Source: "published", Ref: "two"}},
		{Name: "US import", Protocol: "socks5", Origin: proxy.Origin{Source: "import:source"}},
	}
	for _, tc := range []struct {
		name                      string
		members, exclusions, want []string
		policy                    string
		warnings                  []string
	}{
		{"all", []string{MemberAll}, []string{"@endpoint:inbound:one"}, []string{"US two", "US import"}, "", nil},
		{"node group", []string{"@nodeGroup:group"}, []string{"@endpoint:inbound:one"}, []string{TargetDirect}, "", []string{"策略组 test 没有可用节点，已改为 DIRECT"}},
		{"source", []string{"@source:source"}, []string{"@endpoint:import:source:US import|socks5"}, []string{TargetReject}, "reject", []string{"策略组 test 没有可用节点，已改为 REJECT"}},
		{"explicit after dynamic", []string{MemberAll, "@endpoint:inbound:one"}, []string{"@endpoint:inbound:one"}, []string{"US two", "US import", "US one"}, "", nil},
		{"explicit before dynamic", []string{"@endpoint:inbound:one", MemberAll}, []string{"@endpoint:inbound:one"}, []string{"US one", "US two", "US import"}, "", nil},
		{"explicit only", []string{"@endpoint:inbound:one"}, []string{"@endpoint:inbound:one"}, []string{"US one"}, "", nil},
		{"missing", []string{MemberAll}, []string{"@endpoint:relay:missing"}, []string{"US one", "US two", "US import"}, "", []string{"组「test」的排除项已不存在：@endpoint:relay:missing"}},
		{"multiple dynamic sources", []string{MemberAll, "@nodeGroup:group", "@source:source"}, []string{"@endpoint:inbound:one", "@endpoint:import:source:US import|socks5"}, []string{"US two"}, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := Group{Name: "test", Members: tc.members, ExcludeEndpoints: tc.exclusions, EmptyPolicy: tc.policy, IncludeKeywords: []string{"US"}}
			resolved := Expand(Template{Groups: []Group{group}}, proxies)
			var got []string
			for _, member := range resolved.Groups[0].Members {
				got = append(got, member.Name)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("members=%v, want=%v", got, tc.want)
			}
			var warnings []string
			for _, warning := range resolved.Warnings {
				warnings = append(warnings, warning.Message)
			}
			if !reflect.DeepEqual(warnings, tc.warnings) {
				t.Fatalf("warnings=%v, want=%v", warnings, tc.warnings)
			}
		})
	}
}

func TestExcludeEndpointsOldGroupJSONUnchanged(t *testing.T) {
	const old = `{"name":"test","type":"select","members":["@all"],"testUrl":"","intervalSeconds":0}`
	var group Group
	if err := Decode([]byte(old), &group); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(group)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != old {
		t.Fatalf("got %s, want %s", raw, old)
	}
	if err := json.Unmarshal([]byte(`{"excludeEndpoints":[]}`), &group); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(group)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != old {
		t.Fatalf("empty exclusions changed JSON: %s", raw)
	}
}

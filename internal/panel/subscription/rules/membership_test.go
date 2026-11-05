package rules

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/proxy"
)

func TestMembershipExpansion(t *testing.T) {
	proxies := []proxy.Proxy{
		{Name: "US one", Origin: proxy.Origin{Source: "published", Ref: "one", Node: proxy.Ref{ID: "same"}}},
		{Name: "US maintenance", Origin: proxy.Origin{Source: "published", Ref: "two", Node: proxy.Ref{ID: "same"}}},
		{Name: "Japan", Origin: proxy.Origin{Source: "published", Ref: "three"}},
		{Name: "US relay", Origin: proxy.Origin{Source: "published", Relayed: true, Ref: "rule"}},
		{Name: "US import", Protocol: "socks5", Origin: proxy.Origin{Source: "import:source", Ref: "content-hash"}},
		{Name: "chain-clone", ClientChain: "chain", Origin: proxy.Origin{Source: "published", Ref: "one"}},
	}
	for _, tc := range []struct {
		name  string
		group Group
		want  []string
	}{
		{"include", Group{Members: []string{MemberAll}, IncludeKeywords: []string{" ｕｓ "}}, []string{"US one", "US maintenance", "US relay", "US import"}},
		{"exclude only", Group{Members: []string{MemberAll}, ExcludeKeywords: []string{"US", "chain"}}, []string{"Japan"}},
		{"both", Group{Members: []string{MemberAll, "@endpoint:inbound:one"}, IncludeKeywords: []string{"us"}, ExcludeKeywords: []string{"MAINTENANCE", "relay"}}, []string{"US one", "US import"}},
		{"exact distinct inbounds", Group{Members: []string{"@endpoint:inbound:one", "@endpoint:inbound:two"}}, []string{"US one", "US maintenance"}},
		{"relay and import", Group{Members: []string{"@endpoint:relay:rule", "@endpoint:import:source:US import|socks5"}}, []string{"US relay", "US import"}},
		{"unauthorized reject", Group{Members: []string{"@endpoint:inbound:missing"}, EmptyPolicy: "reject"}, []string{TargetReject}},
		{"unauthorized default", Group{Members: []string{"@endpoint:relay:missing"}}, []string{TargetDirect}},
		{"filtered direct", Group{Members: []string{MemberAll}, IncludeKeywords: []string{"missing"}, EmptyPolicy: "direct"}, []string{TargetDirect}},
		{"builtins and groups unfiltered", Group{Members: []string{TargetDirect, "other"}, IncludeKeywords: []string{"missing"}, ExcludeKeywords: []string{"DIRECT", "other"}}, []string{TargetDirect, "other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.group.Name = "test"
			resolved := Expand(Template{Groups: []Group{tc.group}}, proxies)
			got := []string{}
			for _, member := range resolved.Groups[0].Members {
				got = append(got, member.Name)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if len(tc.want) == 1 && (tc.want[0] == TargetDirect || tc.want[0] == TargetReject) && len(resolved.Warnings) != 1 {
				t.Fatal("missing empty-group warning")
			}
		})
	}
}

func TestMembershipValidationAndOldRoundTrip(t *testing.T) {
	for _, member := range []string{"@endpoint:inbound:unknown", "@endpoint:relay:unknown", "@endpoint:import:source:US name|socks5"} {
		template := Default()
		template.Groups[0].Members = []string{member}
		if err := Validate(context.Background(), template, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*Group)
	}{
		{"unknown policy", func(g *Group) { g.EmptyPolicy = "allow" }},
		{"too many includes", func(g *Group) { g.IncludeKeywords = make([]string, 33) }},
		{"too many excludes", func(g *Group) { g.ExcludeKeywords = make([]string, 33) }},
		{"empty keyword", func(g *Group) { g.IncludeKeywords = []string{""} }},
		{"long keyword", func(g *Group) { g.ExcludeKeywords = []string{strings.Repeat("中", 65)} }},
		{"control keyword", func(g *Group) { g.IncludeKeywords = []string{"US\n"} }},
		{"unknown kind", func(g *Group) { g.Members = []string{"@endpoint:node:a"} }},
		{"empty id", func(g *Group) { g.Members = []string{"@endpoint:inbound:"} }},
		{"long id", func(g *Group) { g.Members = []string{"@endpoint:relay:" + strings.Repeat("a", 129)} }},
		{"empty source", func(g *Group) { g.Members = []string{"@endpoint:import::name|socks5"} }},
		{"empty key", func(g *Group) { g.Members = []string{"@endpoint:import:src:"} }},
		{"missing protocol", func(g *Group) { g.Members = []string{"@endpoint:import:src:name|"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			template := Default()
			tc.change(&template.Groups[0])
			if err := Validate(context.Background(), template, nil); err == nil {
				t.Fatal("accepted invalid membership")
			}
		})
	}
	old := Default()
	raw, _ := json.Marshal(old)
	var decoded Template
	if err := Decode(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := Validate(context.Background(), decoded, nil); err != nil {
		t.Fatal(err)
	}
	decoded.Groups[0].IncludeKeywords = []string{}
	decoded.Groups[0].ExcludeKeywords = []string{}
	after, _ := json.Marshal(decoded)
	if string(raw) != string(after) || strings.Contains(string(after), "emptyPolicy") {
		t.Fatal("old template changed", string(after))
	}
}

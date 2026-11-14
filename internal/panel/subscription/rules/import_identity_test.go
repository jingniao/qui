package rules

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/proxy"
)

func TestImportedEndpointRefIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, kind, displayName, want string
	}{
		{"content", "content", "US original", "import:source:id:stable_node-1"},
		{"content renamed", "content", "id:new|name:US", "import:source:id:stable_node-1"},
		{"url", "url", "US original", "import:source:US original|socks5"},
		{"url colon", "url", "US:west", "import:source:US:west|socks5"},
		{"url pipe", "url", "US|west", "import:source:US|west|socks5"},
		{"url id prefix", "url", "id:stable_node-1", "import:source:id:stable_node-1|socks5"},
		{"url all separators", "url", "id:US|west:1", "import:source:id:US|west:1|socks5"},
		{"unmarked origin", "", "US original", "import:source:US original|socks5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := proxy.Proxy{Name: tc.displayName, Protocol: "socks5", Origin: proxy.Origin{Source: "import:source", Ref: "stable_node-1", ImportKind: tc.kind}}
			if got := EndpointRef(endpoint); got != tc.want {
				t.Fatalf("EndpointRef=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestImportedEndpointStableExpansion(t *testing.T) {
	for _, name := range []string{"US original", "id:renamed|US:west"} {
		t.Run(name, func(t *testing.T) {
			endpoints := []proxy.Proxy{
				{Name: name, Protocol: "socks5", Origin: proxy.Origin{Source: "import:source", Ref: "stable_node-1", ImportKind: "content"}},
				{Name: "other", Protocol: "socks5", Origin: proxy.Origin{Source: "import:source", Ref: "stable_node-2", ImportKind: "content"}},
				{Name: "id:stable_node-1", Protocol: "socks5", Origin: proxy.Origin{Source: "import:airport", Ref: "hash", ImportKind: "url"}},
				{Name: "chain clone", ClientChain: "chain", Origin: proxy.Origin{Source: "import:source", Ref: "stable_node-1", ImportKind: "content"}},
			}
			template := Template{Groups: []Group{
				{Name: "content", Members: []string{"@endpoint:import:source:id:stable_node-1"}},
				{Name: "url", Members: []string{"@endpoint:import:airport:id:stable_node-1|socks5"}},
				{Name: "missing", Members: []string{"@endpoint:import:source:id:missing"}, EmptyPolicy: "reject"},
				{Name: "excluded", Members: []string{"@source:source"}, ExcludeEndpoints: []string{"@endpoint:import:source:id:stable_node-1"}},
			}}
			resolved := Expand(template, endpoints)
			want := [][]Member{
				{{Kind: KindProxy, Name: name}},
				{{Kind: KindProxy, Name: "id:stable_node-1"}},
				{{Kind: KindBuiltin, Name: TargetReject}},
				{{Kind: KindProxy, Name: "other"}},
			}
			for i, group := range resolved.Groups {
				if !reflect.DeepEqual(group.Members, want[i]) {
					t.Errorf("group %s members=%v, want %v", group.Name, group.Members, want[i])
				}
			}
			if got := resolved.EndpointRefs[name]; got != "import:source:id:stable_node-1" {
				t.Errorf("preview ref=%q", got)
			}
			if _, found := resolved.EndpointRefs["chain clone"]; found {
				t.Error("client-chain clone became selectable")
			}
		})
	}
}

func TestImportedEndpointReferenceValidation(t *testing.T) {
	for _, tc := range []struct {
		name, ref string
		valid     bool
	}{
		{"stable id", "source:id:stable_node-1", true},
		{"url", "source:US west|socks5", true},
		{"url colon", "source:US:west|socks5", true},
		{"url pipe", "source:US|west|socks5", true},
		{"url leading pipe", "source:|west|socks5", true},
		{"url id prefix", "source:id:stable_node-1|socks5", true},
		{"url id only name", "source:id:|socks5", true},
		{"url all separators", "source:id:US|west:1|socks5", true},
		{"empty source", ":id:node", false},
		{"missing source separator", "source", false},
		{"empty id", "source:id:", false},
		{"unmarked id", "source:node", false},
		{"id colon", "source:id:node:extra", false},
		{"id space", "source:id:node extra", false},
		{"id slash", "source:id:node/extra", false},
		{"id control", "source:id:node\n", false},
		{"id too long", "source:id:" + strings.Repeat("a", 129), false},
		{"empty name", "source:|socks5", false},
		{"empty protocol", "source:name|", false},
		{"empty final protocol", "source:name|socks5|", false},
		{"control name", "source:name\n|socks5", false},
		{"key too long", "source:" + strings.Repeat("a", 1024) + "|socks5", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, exclude := range []bool{false, true} {
				template := Default()
				ref := "@endpoint:import:" + tc.ref
				if exclude {
					template.Groups[0].ExcludeEndpoints = []string{ref}
				} else {
					template.Groups[0].Members = []string{ref}
				}
				if err := Validate(context.Background(), template, nil); (err == nil) != tc.valid {
					t.Errorf("exclude=%v valid=%v error=%v", exclude, tc.valid, err)
				}
			}
		})
	}
}

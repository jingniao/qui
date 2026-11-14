package singbox

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/proxy"
)

func namedProxy(name, ref, kind string) proxy.Proxy {
	return proxy.Proxy{Name: name, Protocol: "socks5", Server: "example.com", Port: 1080,
		Origin: proxy.Origin{Source: "import:source", Ref: ref, ImportKind: kind, Node: proxy.Ref{Name: "Node"}}}
}

func renderNames(t *testing.T, entries []proxy.Proxy) ([]map[string]any, format.Output) {
	t.Helper()
	out, err := (Exporter{}).Render(format.Profile{Proxies: entries})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
		Endpoints []map[string]any `json:"endpoints"`
	}
	if err := json.Unmarshal(out.Body, &doc); err != nil {
		t.Fatal(err)
	}
	return append(doc.Outbounds, doc.Endpoints...), out
}

func TestContentTagsUseCurrentNameOnlyForContent(t *testing.T) {
	content := namedProxy("新名称", "content-id", "content")
	url := namedProxy("URL name", "url-id", "url")
	built := namedProxy("Built name", "inbound-id", "")
	built.Origin.Source = "published"
	got, out := renderNames(t, []proxy.Proxy{content, url, built})
	want := []string{"新名称", "Node-url-id-socks5", "Node-inbound-id-socks5"}
	if len(got) != len(want) || len(out.Warnings) != 0 {
		t.Fatalf("outbounds=%v warnings=%v", got, out.Warnings)
	}
	for i, tag := range want {
		if got[i]["tag"] != tag {
			t.Errorf("tag[%d]=%v want %q", i, got[i]["tag"], tag)
		}
	}
}

func TestContentTagCollisionsRemainUniqueAndStable(t *testing.T) {
	entries := []proxy.Proxy{
		namedProxy("different display", "url", "url"),
		namedProxy("Node-url-socks5", "id", "content"),
		namedProxy("Node-url-socks5", "id", "content"),
		namedProxy("Node-url-socks5-id", "other", "content"),
	}
	got, out := renderNames(t, entries)
	want := []string{"Node-url-socks5", "Node-url-socks5-id", "Node-url-socks5-id-2", "Node-url-socks5-id-other"}
	if len(got) != len(want) {
		t.Fatalf("outbounds=%v", got)
	}
	for i, tag := range want {
		if got[i]["tag"] != tag {
			t.Errorf("tag[%d]=%v want %q", i, got[i]["tag"], tag)
		}
	}
	_, again := renderNames(t, entries)
	if !reflect.DeepEqual(out.Body, again.Body) {
		t.Fatal("tag allocation changed across renders")
	}
}

func TestWireGuardTagsShareOutboundNamespace(t *testing.T) {
	key, err := ecdh.X25519().NewPrivateKey([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ kind, want string }{
		{"content", "新名称-wg"}, {"url", "Node-wg"}, {"", "Node-wg"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			wg := namedProxy("新名称", "wg", tc.kind)
			wg.Protocol = "wireguard"
			wg.Credential = proxy.Credential{Password: base64.StdEncoding.EncodeToString(key.Bytes()), PublicKey: base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())}
			wg.WireGuard = &proxy.WireGuard{ServerPublicKey: base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), Addresses: []string{"10.0.0.2/32"}}
			got, out := renderNames(t, []proxy.Proxy{namedProxy("新名称", "ordinary", "content"), wg})
			if len(got) != 2 || len(out.Warnings) != 0 || got[1]["tag"] != tc.want {
				t.Fatalf("WireGuard tag=%v warnings=%v", got, out.Warnings)
			}
		})
	}
}

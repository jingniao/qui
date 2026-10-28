package clash

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func fixtureCA(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func wireguard(t *testing.T, name string) proxy.Proxy {
	t.Helper()
	private := make([]byte, 32)
	for index := range private {
		private[index] = byte(index + 1)
	}
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	server := make([]byte, 32)
	for index := range server {
		server[index] = byte(index + 101)
	}
	return proxy.Proxy{Origin: proxy.Origin{Source: "published", Ref: "in-wg", Node: proxy.Ref{ID: "node-3", Name: "Seoul"}, Labels: []string{"nodeGroup:kr"}}, Name: name, Server: "192.0.2.9", Port: 51820, Protocol: "wireguard",
		Credential: proxy.Credential{Password: base64.StdEncoding.EncodeToString(private), PublicKey: base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())},
		WireGuard:  &proxy.WireGuard{ServerPublicKey: base64.StdEncoding.EncodeToString(server), Addresses: []string{"10.99.0.2/32", "2001:db8:99::2/128"}, MTU: 1420}}
}

// fixtures is every protocol × transport × TLS combination the panel publishes.
func fixtures(t *testing.T) []proxy.Proxy {
	t.Helper()
	origin := func(ref, node string) proxy.Origin {
		return proxy.Origin{Source: "published", Ref: ref, Node: proxy.Ref{ID: node, Name: node}, Labels: []string{"nodeGroup:jp"}}
	}
	tls := func(name string) *proxy.TLS { return &proxy.TLS{ServerName: name} }
	ca := fixtureCA(t)
	return []proxy.Proxy{
		{Origin: origin("http", "n1"), Name: "http plain", Server: "203.0.113.10", Port: 8080, Protocol: "http", Credential: proxy.Credential{Username: "alice", Password: "pw"}},
		{Origin: origin("http-tls", "n1"), Name: "http tls", Server: "203.0.113.10", Port: 8443, Protocol: "http", Credential: proxy.Credential{Username: "alice", Password: "pw"}, Security: "tls", TLS: tls("h.example.com")},
		{Origin: origin("socks", "n1"), Name: "socks5 plain", Server: "203.0.113.10", Port: 1080, Protocol: "socks5", Credential: proxy.Credential{Username: "alice", Password: "pw"}},
		{Origin: origin("socks-tls", "n1"), Name: "socks5 tls", Server: "203.0.113.10", Port: 1443, Protocol: "socks5", Credential: proxy.Credential{Username: "alice", Password: "pw"}, Security: "tls", TLS: tls("s.example.com")},
		{Origin: origin("ss-default", "n2"), Name: "ss default cipher", Server: "198.51.100.7", Port: 8388, Protocol: "shadowsocks", Credential: proxy.Credential{Password: "ss-secret"}},
		{Origin: origin("ss-128", "n2"), Name: "ss aes-128-gcm", Server: "198.51.100.7", Port: 8389, Protocol: "shadowsocks", Credential: proxy.Credential{Password: "ss-secret"}, Method: "aes-128-gcm"},
		{Origin: origin("ss-256", "n2"), Name: "ss aes-256-gcm", Server: "198.51.100.7", Port: 8390, Protocol: "shadowsocks", Credential: proxy.Credential{Password: "ss-secret"}, Method: "aes-256-gcm"},
		{Origin: origin("ss-x", "n2"), Name: "ss xchacha20", Server: "198.51.100.7", Port: 8391, Protocol: "shadowsocks", Credential: proxy.Credential{Password: "ss-secret"}, Method: "xchacha20-ietf-poly1305"},
		{Origin: origin("vless-tcp", "n1"), Name: "vless tcp plain", Server: "203.0.113.10", Port: 10000, Protocol: "vless", Credential: proxy.Credential{Password: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}},
		{Origin: origin("vless-tls", "n1"), Name: "vless tcp tls vision", Server: "203.0.113.10", Port: 443, Protocol: "vless", Credential: proxy.Credential{Password: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}, Flow: "xtls-rprx-vision", Security: "tls", TLS: tls("v.example.com")},
		{Origin: origin("vless-ws", "n1"), Name: "vless ws tls", Server: "203.0.113.10", Port: 2443, Protocol: "vless", Credential: proxy.Credential{Password: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}, Security: "tls", TLS: tls("v.example.com"), Transport: &proxy.Transport{Type: "ws", Path: "/ray"}},
		{Origin: origin("vless-ws-plain", "n1"), Name: "vless ws plain", Server: "203.0.113.10", Port: 2080, Protocol: "vless", Credential: proxy.Credential{Password: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}, Transport: &proxy.Transport{Type: "ws", Path: "/ray"}},
		{Origin: origin("vless-grpc", "n1"), Name: "vless grpc tls", Server: "203.0.113.10", Port: 3443, Protocol: "vless", Credential: proxy.Credential{Password: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}, Security: "tls", TLS: tls("v.example.com"), Transport: &proxy.Transport{Type: "grpc", ServiceName: "svc"}},
		{Origin: origin("trojan-tcp", "n1"), Name: "trojan tcp", Server: "203.0.113.10", Port: 4443, Protocol: "trojan", Credential: proxy.Credential{Password: "trojan-secret"}, Security: "tls", TLS: tls("t.example.com")},
		{Origin: origin("trojan-ws", "n1"), Name: "trojan ws", Server: "203.0.113.10", Port: 5443, Protocol: "trojan", Credential: proxy.Credential{Password: "trojan-secret"}, Security: "tls", TLS: tls("t.example.com"), Transport: &proxy.Transport{Type: "ws", Path: "/t"}},
		{Origin: origin("trojan-grpc", "n1"), Name: "trojan grpc", Server: "203.0.113.10", Port: 6443, Protocol: "trojan", Credential: proxy.Credential{Password: "trojan-secret"}, Security: "tls", TLS: tls("t.example.com"), Transport: &proxy.Transport{Type: "grpc", ServiceName: "tsvc"}},
		{Origin: origin("hy2", "n3"), Name: "hysteria2", Server: "2001:db8::1", Port: 443, Protocol: "hysteria2", Credential: proxy.Credential{Password: "hy2-secret"}, Security: "tls", TLS: tls("hy2.example.com"), Transport: &proxy.Transport{Type: "quic"}, QUIC: &proxy.QUIC{UpMbps: 50, DownMbps: 100}},
		{Origin: origin("hy2-pinned", "n3"), Name: "hysteria2 pinned", Server: "2001:db8::2", Port: 8443, Protocol: "hysteria2", Credential: proxy.Credential{Password: "pinned"}, Security: "tls", TLS: &proxy.TLS{ServerName: "p.example.com", PinnedCA: ca}, Transport: &proxy.Transport{Type: "quic"}},
		{Origin: origin("tuic", "n3"), Name: "tuic", Server: "2001:db8::1", Port: 8444, Protocol: "tuic", Credential: proxy.Credential{Username: "6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", Password: "tuic-secret"}, Security: "tls", TLS: tls("tuic.example.com"), QUIC: &proxy.QUIC{CongestionControl: "bbr", UDPRelayMode: "quic"}},
		{Origin: origin("tuic-pinned", "n3"), Name: "tuic pinned", Server: "2001:db8::2", Port: 8445, Protocol: "tuic", Credential: proxy.Credential{Username: "7ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", Password: "pinned"}, Security: "tls", TLS: &proxy.TLS{ServerName: "p.example.com", PinnedCA: ca}},
		wireguard(t, "wireguard"),
	}
}

// richTemplate exercises every rule type, a node group member, an external
// provider and a panel service rule.
func richTemplate() rules.Template {
	template := rules.Default()
	template.DNS = rules.DNS{Enable: true, EnhancedMode: "fake-ip", Nameservers: []string{"223.5.5.5", "https://dns.example/dns-query"}, Fallback: []string{"tls://1.1.1.1"}}
	template.Groups = append(template.Groups, rules.Group{Name: "日本", Type: "select", Members: []string{"@nodeGroup:jp", rules.TargetDirect}}, rules.Group{Name: "空组", Type: "select", Members: []string{"@nodeGroup:none"}})
	template.Providers = []rules.Provider{{Name: "ads", Behavior: "domain", Format: "text", URL: "https://rules.example/ads.txt", IntervalSeconds: 86400}}
	template.Rules = append(template.Rules,
		rules.Rule{ID: "geosite", Type: rules.RuleGeoSite, Payload: "category-ads-all", Target: rules.TargetReject, Enabled: true, Priority: 4},
		rules.Rule{ID: "provider", Type: rules.RuleSet, Payload: "ads", Target: rules.TargetReject, Enabled: true, Priority: 5},
		rules.Rule{ID: "service", Type: rules.RuleSet, Payload: "service:svc-1", Target: "日本", Enabled: true, Priority: 6},
		rules.Rule{ID: "v6", Type: rules.RuleIPCIDR6, Payload: "2001:db8::/32", Target: rules.TargetDirect, NoResolve: true, Enabled: true, Priority: 7},
		rules.Rule{ID: "port", Type: rules.RuleDstPort, Payload: "25/465-587", Target: rules.TargetReject, Enabled: true, Priority: 8},
		rules.Rule{ID: "node", Type: rules.RuleDomain, Payload: "wg.example.com", Target: "node:node-3", Enabled: true, Priority: 9},
	)
	return template
}

func renderWith(t *testing.T, exporter format.Exporter, proxies []proxy.Proxy, template rules.Template) format.Output {
	t.Helper()
	resolved := rules.Expand(template, proxies)
	output, err := exporter.Render(format.Profile{Proxies: proxies, Rules: &resolved, ProviderURL: func(id string) string { return "https://panel.example/sub/TOKEN/rules/" + id }})
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the golden file:\n%s", name, got)
	}
}

// A3.1: golden output for every combination in both profiles.
func TestGoldenProfiles(t *testing.T) {
	proxies := fixtures(t)
	golden(t, "meta.yaml", renderWith(t, Meta{}, proxies, richTemplate()).Body)
	golden(t, "legacy.yaml", renderWith(t, Legacy{}, proxies, richTemplate()).Body)
}

type parsed struct {
	Proxies []map[string]any `yaml:"proxies"`
	Groups  []struct {
		Name    string   `yaml:"name"`
		Proxies []string `yaml:"proxies"`
	} `yaml:"proxy-groups"`
	Providers map[string]map[string]any `yaml:"rule-providers"`
	Rules     []string                  `yaml:"rules"`
}

func parse(t *testing.T, body []byte) parsed {
	t.Helper()
	var document parsed
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

// A3.1: hostile names survive a YAML round trip, collisions with groups or
// reserved names are renamed, and every member and target follows the rename.
func TestHostileNamesRoundTrip(t *testing.T) {
	names := []string{`quote"inside`, "colon:inside", "hash#inside", "中文节点", "newline\ninside", "name: with space", "#hashfirst", "true", "123", "null", `"both ' quotes"`, "PROXY", "direct"}
	proxies := []proxy.Proxy{}
	for index, name := range names {
		proxies = append(proxies, proxy.Proxy{Origin: proxy.Origin{Source: "published", Ref: "in", Node: proxy.Ref{ID: "node-" + string(rune('a'+index))}}, Name: name, Server: "203.0.113.1", Port: 1000 + index, Protocol: "socks5"})
	}
	template := rules.Default()
	template.Rules = append(template.Rules, rules.Rule{ID: "pin", Type: rules.RuleDomain, Payload: "x.example", Target: "node:node-l", Enabled: true, Priority: 4})
	document := parse(t, renderWith(t, Meta{}, proxies, template).Body)
	got := map[string]bool{}
	for _, item := range document.Proxies {
		got[item["name"].(string)] = true
	}
	for _, name := range names[:11] {
		if !got[name] {
			t.Fatalf("name %q did not survive: %v", name, got)
		}
	}
	if !got["PROXY (2)"] || !got["direct (2)"] {
		t.Fatalf("colliding names were not renamed: %v", got)
	}
	members := map[string][]string{}
	for _, group := range document.Groups {
		members[group.Name] = group.Proxies
	}
	if !contains(members["AUTO"], "PROXY (2)") || contains(members["AUTO"], "PROXY") {
		t.Fatalf("AUTO members do not follow the rename: %v", members["AUTO"])
	}
	if !contains(document.Rules, "DOMAIN,x.example,PROXY (2)") {
		t.Fatalf("the rule target does not follow the rename: %v", document.Rules)
	}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// A3.3: the legacy profile carries none of the excluded proxy, rule or provider
// types, and its group graph stays valid even when every proxy is excluded.
func TestLegacyExclusions(t *testing.T) {
	output := renderWith(t, Legacy{}, fixtures(t), richTemplate())
	document := parse(t, output.Body)
	for _, item := range document.Proxies {
		switch item["type"] {
		case "ss", "socks5", "http", "trojan":
		default:
			t.Fatalf("legacy profile carries %v", item["type"])
		}
	}
	if document.Providers != nil {
		t.Fatal("legacy profile carries rule providers")
	}
	for _, rule := range document.Rules {
		if strings.HasPrefix(rule, "GEOSITE,") || strings.HasPrefix(rule, "RULE-SET,") {
			t.Fatalf("legacy profile carries %s", rule)
		}
	}
	if len(output.Warnings) == 0 {
		t.Fatal("dropped proxies and rules produced no warnings")
	}
	only := []proxy.Proxy{fixtures(t)[8], fixtures(t)[16]}
	empty := parse(t, renderWith(t, Legacy{}, only, rules.Default()).Body)
	if len(empty.Proxies) != 0 {
		t.Fatalf("excluded proxies survived: %v", empty.Proxies)
	}
	known := map[string]bool{"DIRECT": true, "REJECT": true}
	for _, group := range empty.Groups {
		known[group.Name] = true
	}
	for _, group := range empty.Groups {
		if len(group.Proxies) == 0 {
			t.Fatalf("group %s is empty", group.Name)
		}
		for _, member := range group.Proxies {
			if !known[member] {
				t.Fatalf("group %s refers to %q", group.Name, member)
			}
		}
	}
}

// The service rule provider points at the user's own token link, never at an
// admin API, and its name comes from the id.
func TestServiceProviderLink(t *testing.T) {
	document := parse(t, renderWith(t, Meta{}, fixtures(t), richTemplate()).Body)
	provider, ok := document.Providers["qui-service-svc-1"]
	if !ok || provider["url"] != "https://panel.example/sub/TOKEN/rules/svc-1" || provider["behavior"] != "classical" || provider["format"] != "text" {
		t.Fatalf("service provider %v", document.Providers)
	}
}

// A3.2: the profiles, every preset and the hostile names pass `mihomo -t`.
// The CI cores job sets MIHOMO_BINARY and a MIHOMO_HOME seeded with pinned,
// digest-checked GeoIP/GeoSite/MMDB files; elsewhere the test is skipped.
func TestMihomoAcceptsTheProfiles(t *testing.T) {
	binary, home := os.Getenv("MIHOMO_BINARY"), os.Getenv("MIHOMO_HOME")
	if binary == "" || home == "" {
		t.Skip("MIHOMO_BINARY and MIHOMO_HOME are not set")
	}
	cases := map[string][]byte{
		"meta.yaml":   renderWith(t, Meta{}, fixtures(t), richTemplate()).Body,
		"legacy.yaml": renderWith(t, Legacy{}, fixtures(t), richTemplate()).Body,
	}
	for _, preset := range rules.Presets() {
		cases["preset-"+preset.ID+".yaml"] = renderWith(t, Meta{}, fixtures(t), preset.Template).Body
	}
	directory := t.TempDir()
	for name, body := range cases {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(binary, "-t", "-d", home, "-f", path).CombinedOutput()
		if err != nil || !strings.Contains(string(output), "test is successful") {
			t.Fatalf("mihomo -t %s: %v\n%s", name, err, output)
		}
	}
}

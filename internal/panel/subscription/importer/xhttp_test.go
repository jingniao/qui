package importer

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

func xhttpFixtures(t *testing.T) []proxy.Proxy {
	t.Helper()
	modes := []string{"auto", "auto", "auto", "auto", "packet-up", "packet-up", "packet-up", "stream-up", "stream-up", "stream-one", "stream-one"}
	lines := []string{}
	for i, mode := range modes {
		u, _ := url.Parse(realityURI("tcp", ""))
		u.User = url.User(fmt.Sprintf("11111111-1111-4111-8111-%012d", i+1))
		u.Host = fmt.Sprintf("127.0.0.1:%d", 21001+i)
		u.Fragment = fmt.Sprintf("S0-XH-%02d", i+1)
		q := u.Query()
		q.Set("type", "xhttp")
		q.Set("path", "/s0")
		q.Set("host", "transport.example.com")
		q.Set("mode", mode)
		q.Set("alpn", "h2")
		u.RawQuery = q.Encode()
		lines = append(lines, u.String())
	}
	result, entries := Analyze([]byte(strings.Join(lines, "\n")))
	if result.Outcome != "ok" || len(entries) != 11 {
		t.Fatalf("11 synthetic fixtures: %+v", result)
	}
	return entries
}

func TestXHTTPFixturesRoundTripAndFormatFiltering(t *testing.T) {
	entries := xhttpFixtures(t)
	for _, p := range entries {
		if p.TLS.ServerName == p.Transport.XHTTP.Host {
			t.Fatal("transport host merged with SNI")
		}
		link, err := uri.Link(p)
		if err != nil {
			t.Fatal(err)
		}
		_, again := Analyze([]byte(link))
		if len(again) != 1 || !reflect.DeepEqual(p, again[0]) {
			t.Fatal("xhttp URI settings lost")
		}
	}
	for _, exporter := range []format.Exporter{clash.Meta{}, singbox.Exporter{}, clash.Legacy{}} {
		out, err := exporter.Render(format.Profile{Proxies: entries})
		if err != nil {
			t.Fatal(err)
		}
		expected := 0
		if exporter.ID() == "clash" {
			expected = 4
		}
		// Count concrete endpoint server fields, rather than names repeated in groups.
		marker := "server: 127.0.0.1"
		if exporter.ID() == "sing-box" {
			marker = `"server": "127.0.0.1"`
		}
		if count := strings.Count(string(out.Body), marker); count != expected || len(out.Warnings) < 11-expected {
			t.Fatalf("%s export count=%d expected=%d warnings=%d", exporter.ID(), count, expected, len(out.Warnings))
		}
		if exporter.ID() == "clash" && (!strings.Contains(string(out.Body), "xhttp-opts:") || !strings.Contains(string(out.Body), "transport.example.com")) {
			t.Fatal("typed xhttp mapping lost")
		}
	}
}

func TestXHTTPEmptyPathClashAndRejectedExtensions(t *testing.T) {
	p := xhttpFixtures(t)[0]
	p.Transport.XHTTP.Path = ""
	out, err := (clash.Meta{}).Render(format.Profile{Proxies: []proxy.Proxy{p}})
	if err != nil {
		t.Fatal(err)
	}
	_, parsed := Analyze(out.Body)
	if len(parsed) != 1 || parsed[0].Transport.XHTTP.Path != "" || parsed[0].Transport.XHTTP.Host != p.Transport.XHTTP.Host {
		t.Fatal("Clash empty path or host changed")
	}
	link, err := uri.Link(p)
	if err != nil || !strings.Contains(link, "path=") {
		t.Fatal("explicit empty URI path omitted")
	}
	for _, test := range []struct{ key, value, code string }{
		{"mode", "", "unsupported_xhttp_mode"},
		{"mode", "unknown", "unsupported_xhttp_mode"}, {"flow", "xtls-rprx-vision", "invalid_protocol_combination"},
		{"extra", `{"private-fixture-secret":"value"}`, "unsupported_option"},
		{"extra", strings.Repeat("x", 4097), "oversized_entry"}, {"host", "bad host", "invalid_xhttp_settings"},
	} {
		u, _ := url.Parse(link)
		q := u.Query()
		q.Set(test.key, test.value)
		u.RawQuery = q.Encode()
		result, entries := Analyze([]byte(u.String()))
		if len(entries) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != test.code {
			t.Fatalf("%s: %+v", test.key, result)
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "private-fixture-secret") {
			t.Fatal("extra data leaked into reason")
		}
	}
	for _, transport := range []string{"tcp", "ws", "grpc"} {
		for _, field := range []string{"host", "mode"} {
			u, _ := url.Parse(realityURI(transport, ""))
			q := u.Query()
			q.Set(field, "")
			u.RawQuery = q.Encode()
			result, entries := Analyze([]byte(u.String()))
			if len(entries) != 0 || result.Diagnostics[0].Code != "invalid_protocol_combination" {
				t.Fatalf("%s silently dropped %s", transport, field)
			}
		}
	}
	clashExtra := "proxies:\n- name: extra\n  type: vless\n  server: example.net\n  port: 443\n  uuid: " + realityUUID + "\n  tls: true\n  servername: example.com\n  client-fingerprint: chrome\n  reality-opts: {public-key: " + realityKey + ", short-id: ''}\n  network: xhttp\n  xhttp-opts: {path: /, mode: auto, extra: {private-fixture-secret: value}}\n"
	result, entries := Analyze([]byte(clashExtra))
	if len(entries) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Field != "transport.xhttp.extra" {
		t.Fatalf("Clash extra not specifically rejected: %+v", result)
	}
	result, entries = Analyze([]byte(strings.Replace(clashExtra, "{private-fixture-secret: value}", "null", 1)))
	if len(entries) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Field != "transport.xhttp.extra" {
		t.Fatal("explicit null extra silently accepted")
	}
	// Other binary formats cannot silently drop the new transport. The ordinary
	// endpoint survives next to all unsupported xhttp entries.
	ordinary := proxy.Proxy{Name: "keep", Protocol: "socks5", Server: "ordinary.example.com", Port: 1080}
	output, err := (singbox.Exporter{}).Render(format.Profile{Proxies: []proxy.Proxy{p, ordinary}})
	if err != nil || !strings.Contains(string(output.Body), "ordinary.example.com") || strings.Contains(string(output.Body), "127.0.0.1") {
		t.Fatal("sing-box omission damaged ordinary endpoint")
	}
}

func TestXHTTPTransportDetailsAndRejectPolicySurviveFiltering(t *testing.T) {
	p := xhttpFixtures(t)[0]
	p.Server = "2001:db8::1"
	p.Transport.XHTTP.Path = "/a path/?q=x+y&v=1"
	link, err := uri.Link(p)
	if err != nil {
		t.Fatal(err)
	}
	_, again := Analyze([]byte(link))
	if len(again) != 1 || !reflect.DeepEqual(p, again[0]) {
		t.Fatal("IPv6/path query escapes changed transport")
	}
	// No h2 evidence means no Mihomo xhttp output, even in auto mode.
	p.TLS.ALPN = nil
	resolved := rules.Resolved{Groups: []rules.ResolvedGroup{{Name: "strict", Type: "select", EmptyPolicy: "reject", Members: []rules.Member{{Kind: rules.KindProxy, Name: p.Name}}}}}
	out, err := (clash.Meta{}).Render(format.Profile{Proxies: []proxy.Proxy{p}, Rules: &resolved})
	if err != nil || strings.Contains(string(out.Body), "2001:db8::1") || !strings.Contains(string(out.Body), "REJECT") {
		t.Fatal("xhttp omission relaxed explicit REJECT")
	}
	// A subsequent client-chain hop cannot outlive an omitted xhttp first hop.
	p.ClientChain = "fixture"
	next := proxy.Proxy{Name: "next", Protocol: "socks5", Server: "orphan.example.com", Port: 1080, DialerProxy: p.Name, ClientChain: "fixture"}
	out, err = (clash.Meta{}).Render(format.Profile{Proxies: []proxy.Proxy{p, next}})
	if err != nil || strings.Contains(string(out.Body), "orphan.example.com") {
		t.Fatal("xhttp filter left dependent chain hop")
	}
}

func TestVisionRejectedForTLSGRPCAndXHTTP(t *testing.T) {
	for _, transport := range []string{"grpc", "xhttp"} {
		u, _ := url.Parse(realityURI("grpc", "xtls-rprx-vision"))
		q := u.Query()
		q.Set("type", transport)
		q.Set("security", "tls")
		q.Del("pbk")
		q.Del("sid")
		q.Del("serviceName")
		if transport == "xhttp" {
			q.Set("mode", "auto")
		}
		u.RawQuery = q.Encode()
		result, entries := Analyze([]byte(u.String()))
		if len(entries) != 0 || result.Diagnostics[0].Code != "invalid_protocol_combination" {
			t.Fatalf("TLS %s Vision accepted", transport)
		}
	}
}

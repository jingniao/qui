package importer

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

const realityKey = "VddTg9EFbwIYD3ymir-1P0xUxP74eFM_9UmM1e7bEwY"
const realityUUID = "11111111-1111-4111-8111-111111111111"

func realityURI(transport, flow string) string {
	q := url.Values{"encryption": {"none"}, "security": {"reality"}, "sni": {"example.com"}, "fp": {"chrome"}, "pbk": {realityKey}, "sid": {""}, "type": {transport}}
	if flow != "" {
		q.Set("flow", flow)
	}
	if transport == "grpc" {
		q.Set("serviceName", "s0 grpc")
	}
	return "vless://" + realityUUID + "@example.net:443?" + q.Encode() + "#reality"
}

func TestRealityURIAndBase64RoundTrip(t *testing.T) {
	for _, transport := range []string{"tcp", "grpc"} {
		for _, flow := range []string{"", "xtls-rprx-vision"} {
			if transport == "grpc" && flow != "" {
				continue
			}
			input := realityURI(transport, flow)
			result, entries := Analyze([]byte(input))
			if result.Outcome != "ok" || len(entries) != 1 {
				t.Fatalf("parse: %+v", result)
			}
			p := entries[0]
			if p.Reality.ShortID == nil || *p.Reality.ShortID != "" || p.TLS.ClientFingerprint != "chrome" || p.TLS.Fingerprint != "" {
				t.Fatalf("settings: %+v", p)
			}
			link, err := uri.Link(p)
			if err != nil {
				t.Fatal(err)
			}
			if transport == "grpc" && !strings.Contains(link, "serviceName=s0%20grpc") {
				t.Fatalf("GR02 must be escaped: %s", link)
			}
			_, again := Analyze([]byte(link))
			if len(again) != 1 || !reflect.DeepEqual(p, again[0]) {
				t.Fatalf("roundtrip mismatch: %#v / %#v", p, again)
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(input))
			_, decoded := Analyze([]byte(encoded))
			if len(decoded) != 1 || !reflect.DeepEqual(p, decoded[0]) {
				t.Fatal("Base64 differs from URI")
			}
		}
	}
}

func TestRealityDiagnosticsAndPrivacy(t *testing.T) {
	base, _ := url.Parse(realityURI("tcp", ""))
	for _, test := range []struct{ name, field, value, code string }{
		{"key", "pbk", "private-fixture-secret", "invalid_reality_key"},
		{"short id", "sid", "123", "invalid_reality_short_id"},
		{"fingerprint", "fp", "private-fixture-secret", "invalid_fingerprint"},
		{"encryption", "encryption", "aes", "invalid_encryption"},
		{"alias", "servername", "other.example.com", "conflicting_alias"},
		{"flow", "flow", "private-fixture-secret", "invalid_protocol_combination"},
	} {
		t.Run(test.name, func(t *testing.T) {
			u := *base
			q := u.Query()
			q.Set(test.field, test.value)
			u.RawQuery = q.Encode()
			result, entries := Analyze([]byte(u.String()))
			if len(entries) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != test.code {
				t.Fatalf("%+v", result)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "private-fixture-secret") || strings.Contains(string(raw), realityUUID) || strings.Contains(string(raw), realityKey) {
				t.Fatal("diagnostic leaked endpoint material")
			}
		})
	}
	for _, raw := range []string{
		strings.Replace(realityURI("tcp", ""), "&sid=", "&", 1),
		realityURI("grpc", "xtls-rprx-vision"),
	} {
		result, entries := Analyze([]byte(raw))
		if len(entries) != 0 || result.Outcome != "failed" {
			t.Fatalf("invalid settings accepted: %+v", result)
		}
	}
	q := base.Query()
	q.Add("fp", "firefox")
	base.RawQuery = q.Encode()
	result, _ := Analyze([]byte(base.String()))
	if result.Diagnostics[0].Code != "conflicting_alias" {
		t.Fatal(result)
	}
}

func TestRealityClashAndMeasuredExportCapabilities(t *testing.T) {
	text := "proxies:\n- name: reality\n  type: vless\n  server: example.net\n  port: 443\n  uuid: " + realityUUID + "\n  tls: true\n  servername: example.com\n  client-fingerprint: chrome\n  reality-opts:\n    public-key: " + realityKey + "\n    short-id: ''\n  flow: xtls-rprx-vision\n"
	result, entries := Analyze([]byte(text))
	if len(entries) != 1 || result.Outcome != "ok" {
		t.Fatal(result)
	}
	p := entries[0]
	for _, exporter := range []format.Exporter{clash.Meta{}, singbox.Exporter{}} {
		out, err := exporter.Render(format.Profile{Proxies: []proxy.Proxy{p}})
		if err != nil || len(out.Warnings) != 0 || !strings.Contains(string(out.Body), realityKey) {
			t.Fatalf("%s: %s %+v %v", exporter.ID(), out.Body, out.Warnings, err)
		}
		if !strings.Contains(string(out.Body), "chrome") {
			t.Fatal("ClientHello fingerprint lost")
		}
	}
	ordinary := proxy.Proxy{Name: "ordinary", Protocol: "socks5", Server: "example.org", Port: 1080}
	_, grpc := Analyze([]byte(realityURI("grpc", "")))
	for _, exporter := range []format.Exporter{clash.Legacy{}, singbox.Exporter{}} {
		out, err := exporter.Render(format.Profile{Proxies: []proxy.Proxy{ordinary, grpc[0]}})
		if err != nil || len(out.Warnings) == 0 || strings.Contains(string(out.Body), realityKey) || !strings.Contains(string(out.Body), "example.org") {
			t.Fatalf("per-entry omission %s: %s %v", exporter.ID(), out.Body, err)
		}
	}
	_, spider := Analyze([]byte(strings.Replace(realityURI("tcp", "xtls-rprx-vision"), "#reality", "&spx=%2Fsearch%3Fq%3Dx#reality", 1)))
	if len(spider) != 1 || spider[0].Reality.SpiderX == nil {
		t.Fatal("spider path not retained")
	}
	if link, err := uri.Link(spider[0]); err != nil || !strings.Contains(link, "spx=") {
		t.Fatal("spider URI lost")
	}
	// A removed chain hop cannot leave a surviving dependent proxy or silently
	// relax an explicit REJECT empty-group policy.
	grpc[0].ClientChain = "fixture"
	grpc[0].Transport = &proxy.Transport{Type: "xhttp", XHTTP: &proxy.XHTTPOptions{Mode: "auto"}}
	grpc[0].TLS.ALPN = []string{"http/1.1"}
	ordinary.DialerProxy = grpc[0].Name
	ordinary.ClientChain = "fixture"
	resolved := rules.Resolved{Groups: []rules.ResolvedGroup{{Name: "strict", Type: "select", EmptyPolicy: "reject", Members: []rules.Member{{Kind: rules.KindProxy, Name: ordinary.Name}}}}}
	out, err := (clash.Meta{}).Render(format.Profile{Proxies: []proxy.Proxy{grpc[0], ordinary}, Rules: &resolved})
	if err != nil || strings.Contains(string(out.Body), "example.org") || !strings.Contains(string(out.Body), "REJECT") {
		t.Fatal("omission left an orphan or relaxed REJECT")
	}
	// Corrupt data is an error, not a valid capability omission.
	corrupt := p
	bad := *p.Reality
	bad.PublicKey = "corrupted"
	corrupt.Reality = &bad
	for _, exporter := range []format.Exporter{uri.URI{}, clash.Meta{}, singbox.Exporter{}} {
		if _, err := exporter.Render(format.Profile{Proxies: []proxy.Proxy{corrupt}}); err == nil {
			t.Fatalf("%s hid internal corruption", exporter.ID())
		}
	}
}

func TestRealityEncryptedPersistenceAndResultArrayValidation(t *testing.T) {
	s, _, _ := storeFixture(t)
	key, _, err := s.Save(context.Background(), "", Input{Name: "Reality", Content: realityURI("tcp", "xtls-rprx-vision"), Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	var sealed []byte
	if err = s.Database.QueryRow("SELECT payload_encrypted FROM imported_proxies WHERE source_id=?", key).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), realityUUID) {
		t.Fatal("plaintext credential stored")
	}
	raw, err := s.Vault.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	var p proxy.Proxy
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.Reality == nil || p.Reality.ShortID == nil || *p.Reality.ShortID != "" || p.TLS.ClientFingerprint != "chrome" {
		t.Fatal("encrypted roundtrip lost settings")
	}
	sources, err := s.List(context.Background())
	if err != nil || len(sources) != 1 {
		t.Fatal(err)
	}
	result := sources[0].LatestResult
	encoded, _ := json.Marshal(result)
	if err = ValidateResult(encoded); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(string(encoded), `"diagnostics":[]`, `"diagnostics":null`, 1), strings.Replace(string(encoded), `"diagnostics":[],`, "", 1)} {
		if ValidateResult([]byte(bad)) == nil {
			t.Fatal("null/omitted array accepted")
		}
	}
}

func TestOrdinaryTLSFingerprintRoundTripAndPinOmissions(t *testing.T) {
	input := "vless://" + realityUUID + "@example.net:443?encryption=none&security=tls&sni=example.com&fp=chrome&alpn=h2%2Chttp%2F1.1&type=tcp#tls"
	_, entries := Analyze([]byte(input))
	if len(entries) != 1 {
		t.Fatal("ordinary TLS client fingerprint rejected")
	}
	p := entries[0]
	link, err := uri.Link(p)
	if err != nil {
		t.Fatal(err)
	}
	_, again := Analyze([]byte(link))
	if len(again) != 1 || !reflect.DeepEqual(p, again[0]) {
		t.Fatal("ordinary TLS fingerprint or ALPN lost")
	}
	for _, exporter := range []format.Exporter{clash.Meta{}, singbox.Exporter{}} {
		out, err := exporter.Render(format.Profile{Proxies: entries})
		if err != nil || !strings.Contains(string(out.Body), "chrome") || !strings.Contains(string(out.Body), "h2") {
			t.Fatalf("%s lost TLS settings", exporter.ID())
		}
	}
	p.TLS.Fingerprint = strings.Repeat("ab", 32)
	out, err := (clash.Meta{}).Render(format.Profile{Proxies: []proxy.Proxy{p}})
	if err != nil || !strings.Contains(string(out.Body), "client-fingerprint: chrome") || !strings.Contains(string(out.Body), "fingerprint: "+p.TLS.Fingerprint) {
		t.Fatal("certificate pin and ClientHello conflated")
	}
	for _, exporter := range []format.Exporter{uri.URI{}, singbox.Exporter{}} {
		out, err := exporter.Render(format.Profile{Proxies: []proxy.Proxy{p, {Name: "keep", Protocol: "socks5", Server: "ordinary.example.com", Port: 1080}}})
		if err != nil || len(out.Warnings) != 1 || strings.Contains(string(out.Body), "example.net") || !strings.Contains(string(out.Body), "ordinary.example.com") {
			t.Fatal("certificate pin silently dropped instead of omitting only its endpoint")
		}
	}
}

package desiredstate

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func testRealityClient(t *testing.T) *RealityClientSettings {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &RealityClientSettings{PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: "0123abcd", SpiderX: "/"}
}

func TestOutboundClientSettingsValidation(t *testing.T) {
	reality := testRealityClient(t)
	valid := []struct {
		name, protocol string
		settings       ProtocolSettings
	}{
		{"tls alpn and fingerprint", "vless", ProtocolSettings{Security: "tls", ServerName: "site.example", ALPN: []string{"h2", "http/1.1"}, ClientFingerprint: "chrome"}},
		{"trojan ws host", "trojan", ProtocolSettings{Security: "tls", Transport: "ws", Path: "/ws", WSHost: "cdn.example.com", ClientFingerprint: "firefox"}},
		{"plain ws host", "vless", ProtocolSettings{Transport: "ws", WSHost: "cdn.example.com"}},
		{"reality tcp vision", "vless", ProtocolSettings{Security: SecurityReality, Flow: "xtls-rprx-vision", ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: reality}},
		{"reality without short id", "vless", ProtocolSettings{Security: SecurityReality, ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: &RealityClientSettings{PublicKey: reality.PublicKey}}},
		{"reality grpc", "vless", ProtocolSettings{Security: SecurityReality, Transport: "grpc", ServiceName: "svc", ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: reality}},
		{"reality xhttp", "vless", ProtocolSettings{Security: SecurityReality, Transport: TransportXHTTP, ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: reality, XHTTP: &XHTTPSettings{Path: "/x", Mode: XHTTPModeAuto}}},
		{"tls xhttp", "vless", ProtocolSettings{Security: "tls", Transport: TransportXHTTP, ServerName: "site.example", XHTTP: &XHTTPSettings{Host: "site.example", Path: "/x", Mode: XHTTPModeStreamOne}}},
	}
	for _, test := range valid {
		if err := ValidateSettings(test.protocol, test.settings, false); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
	lowOrder := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	invalid := []struct {
		name, protocol string
		inbound        bool
		settings       ProtocolSettings
		want           string
	}{
		{"alpn on an inbound", "vless", true, ProtocolSettings{Security: "tls", ALPN: []string{"h2"}}, "require an outbound"},
		{"reality client on an inbound", "vless", true, ProtocolSettings{Security: SecurityReality, RealityClient: reality}, "server settings"},
		{"server reality on an outbound", "vless", false, ProtocolSettings{Security: SecurityReality, ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: reality, Reality: &RealityServerSettings{}}, "require an inbound"},
		{"alpn without tls", "vless", false, ProtocolSettings{ALPN: []string{"h2"}}, "TLS or Reality"},
		{"alpn h3", "vless", false, ProtocolSettings{Security: "tls", ALPN: []string{"h3"}}, "h2 or http/1.1"},
		{"duplicate alpn", "vless", false, ProtocolSettings{Security: "tls", ALPN: []string{"h2", "h2"}}, "h2 or http/1.1"},
		{"alpn on hysteria2", "hysteria2", false, ProtocolSettings{Security: "tls", ALPN: []string{"h2"}}, "VLESS or Trojan"},
		{"unknown fingerprint", "vless", false, ProtocolSettings{Security: "tls", ClientFingerprint: "netscape"}, "unsupported client fingerprint"},
		{"fingerprint without tls", "vless", false, ProtocolSettings{ClientFingerprint: "chrome"}, "TLS or Reality"},
		{"ws host on tcp", "vless", false, ProtocolSettings{WSHost: "cdn.example.com"}, "ws transport"},
		{"ws host with port", "vless", false, ProtocolSettings{Transport: "ws", WSHost: "cdn.example.com:443"}, "ws transport"},
		{"reality without client half", "vless", false, ProtocolSettings{Security: SecurityReality, ServerName: "www.example.com", ClientFingerprint: "chrome"}, "client settings"},
		{"reality without fingerprint", "vless", false, ProtocolSettings{Security: SecurityReality, ServerName: "www.example.com", RealityClient: reality}, "client fingerprint"},
		{"reality without server name", "vless", false, ProtocolSettings{Security: SecurityReality, ClientFingerprint: "chrome", RealityClient: reality}, "server name"},
		{"reality on trojan", "trojan", false, ProtocolSettings{Security: SecurityReality, ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: reality}, "VLESS outbound"},
		{"reality low-order key", "vless", false, ProtocolSettings{Security: SecurityReality, ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: &RealityClientSettings{PublicKey: lowOrder}}, "public key"},
		{"reality odd short id", "vless", false, ProtocolSettings{Security: SecurityReality, ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: &RealityClientSettings{PublicKey: reality.PublicKey, ShortID: "abc"}}, "short id"},
		{"reality with a pinned CA", "vless", false, ProtocolSettings{Security: SecurityReality, ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: reality, TrustedCAPEM: "x"}, "certificate material"},
		{"xhttp without tls", "vless", false, ProtocolSettings{Transport: TransportXHTTP, XHTTP: &XHTTPSettings{Path: "/x", Mode: XHTTPModeAuto}}, "TLS or Reality"},
		{"vision over xhttp", "vless", false, ProtocolSettings{Security: "tls", Flow: "xtls-rprx-vision", Transport: TransportXHTTP, XHTTP: &XHTTPSettings{Path: "/x", Mode: XHTTPModeAuto}}, "TCP with TLS or Reality"},
		{"client fields on direct", "direct", false, ProtocolSettings{WSHost: "cdn.example.com"}, "direct/block"},
	}
	for _, test := range invalid {
		if err := ValidateSettings(test.protocol, test.settings, test.inbound); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

func TestOutboundSettingsCombinationAndGate(t *testing.T) {
	reality := testRealityClient(t)
	legacy := ProtocolSettings{Security: "tls", ServerName: "site.example"}
	if _, required := OutboundSettingsCombination("vless", legacy); required {
		t.Fatal("a legacy outbound must not need a report")
	}
	settings := ProtocolSettings{Security: SecurityReality, Flow: "xtls-rprx-vision", ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: reality}
	combination, required := OutboundSettingsCombination("vless", settings)
	if !required || combination != (SettingsCombination{Protocol: "vless", Direction: DirectionOutbound, Security: SecurityReality, Transport: "tcp", Flow: "xtls-rprx-vision"}) {
		t.Fatalf("combination %+v %v", combination, required)
	}
	if err := RequireOutboundSettings(CoreXray, CoreSettingsCapability{}, "vless", legacy); err != nil {
		t.Fatal("legacy outbound refused", err)
	}
	if err := RequireOutboundSettings(CoreXray, CoreSettingsCapability{}, "vless", settings); err == nil {
		t.Fatal("an Agent without a report accepted client settings")
	}
	inbound := combination
	inbound.Direction = DirectionInbound
	onlyInbound := CoreSettingsCapability{Core: CoreXray, Version: SettingsContractVersion, Combinations: []SettingsCombination{inbound}}
	if err := RequireOutboundSettings(CoreXray, onlyInbound, "vless", settings); err == nil {
		t.Fatal("an inbound report authorized an outbound")
	}
	reported := CoreSettingsCapability{Core: CoreXray, Version: SettingsContractVersion, Combinations: OutboundClientCombinations()}
	if err := RequireOutboundSettings(CoreXray, reported, "vless", settings); err != nil {
		t.Fatal(err)
	}
	xhttp := ProtocolSettings{Security: "tls", Transport: TransportXHTTP, ServerName: "site.example", XHTTP: &XHTTPSettings{Path: "/x", Mode: XHTTPModeAuto}}
	singbox := reported
	singbox.Core = CoreSingBox
	if err := RequireOutboundSettings(CoreSingBox, singbox, "vless", xhttp); err == nil || !strings.Contains(err.Error(), "xhttp") {
		t.Fatalf("sing-box accepted an xhttp outbound: %v", err)
	}
	grpc := ProtocolSettings{Security: SecurityReality, Transport: "grpc", ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: reality}
	if err := RequireOutboundSettings(CoreSingBox, singbox, "vless", grpc); err == nil {
		t.Fatal("sing-box accepted Reality over gRPC")
	}
	for _, c := range OutboundClientCombinations() {
		if !c.Valid() || c.Direction != DirectionOutbound {
			t.Fatalf("invalid outbound combination %s", c)
		}
	}
}

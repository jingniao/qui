package desiredstate

import (
	"strings"
	"testing"
)

// The protocol matrix is the single basis the API, the UI and the server checks
// read (plan 6.5). These tests pin the official-core facts so a fork-only
// capability or a wrong per-core direction cannot come back unnoticed.
func TestProtocolSupportTablePinsOfficialCoreMatrix(t *testing.T) {
	inbound := strings.Join(SupportedInboundProtocols(), ",")
	if inbound != "http,socks5,shadowsocks,vless,trojan,wireguard,hysteria2,tuic" {
		t.Fatal("inbound protocol basis changed", inbound)
	}
	outbound := strings.Join(SupportedOutboundProtocols(), ",")
	if outbound != "http,socks5,shadowsocks,vless,trojan,wireguard,hysteria2,tuic,direct,block" {
		t.Fatal("outbound protocol basis changed", outbound)
	}
	for _, row := range ProtocolSupportTable() {
		for _, dimension := range append(append(append([]string{}, row.Implemented...), row.Pending...), row.NotApplicable...) {
			if dimension == "egress_proof" || dimension == "inbound_bytes" {
				t.Fatal("fork-only capability dimension returned", row.Protocol, dimension)
			}
		}
		if _, ok := row.Cores[CoreXray]; !ok {
			if row.Protocol != "hysteria2" && row.Protocol != "tuic" {
				t.Fatal("missing Xray row", row.Protocol)
			}
		}
		if _, ok := row.Cores[CoreSingBox]; !ok {
			t.Fatal("missing sing-box row", row.Protocol)
		}
	}
}

func TestCoreSupportMatchesOfficialCoreBehaviour(t *testing.T) {
	// WireGuard: Xray has an inbound from major 25 on, official sing-box has the
	// outbound only (plan D4).
	if !CoreSupportsInbound(CoreXray, "wireguard") || !CoreSupportsOutbound(CoreXray, "wireguard") {
		t.Fatal("Xray WireGuard support lost")
	}
	if CoreSupportsInbound(CoreSingBox, "wireguard") || !CoreSupportsOutbound(CoreSingBox, "wireguard") {
		t.Fatal("sing-box WireGuard must be outbound only")
	}
	// QUIC protocols: sing-box only, and only in a with_quic build.
	for _, protocol := range []string{"hysteria2", "tuic"} {
		if support, ok := CoreSupportFor(CoreXray, protocol); !ok || support != (CoreSupport{}) {
			t.Fatal("Xray must not carry", protocol, support)
		}
		if !CoreSupportsInbound(CoreSingBox, protocol) || !CoreSupportsOutbound(CoreSingBox, protocol) {
			t.Fatal("sing-box QUIC protocol lost", protocol)
		}
	}
	// HTTP carries TCP only on both cores.
	http, ok := CoreSupportFor(CoreSingBox, "http")
	if !ok || http != (CoreSupport{Inbound: true, Outbound: true, TCP: true}) {
		t.Fatal("sing-box HTTP support changed")
	}
	// direct and block are outbound-only on both cores.
	for _, protocol := range []string{"direct", "block"} {
		if SupportsInbound(protocol) || !SupportsOutbound(protocol) {
			t.Fatal("direction of", protocol, "changed")
		}
		if CoreSupportsInbound(CoreXray, protocol) || CoreSupportsInbound(CoreSingBox, protocol) {
			t.Fatal(protocol, "must not be an inbound")
		}
	}
	// An unknown core and an unknown protocol carry nothing.
	if _, ok := CoreSupportFor("singbox", "vless"); ok {
		t.Fatal("core name must match the Agent report")
	}
	if CoreSupportsInbound(CoreXray, "vmess") || CoreSupportsOutbound(CoreSingBox, "vmess") {
		t.Fatal("unsupported protocol accepted")
	}
}

package published

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
)

func TestPinnedTLSSubscriptionFormats(t *testing.T) {
	ca, err := os.ReadFile("../../format/clash/testdata/ca.pem")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(ca)
	digest := sha256.Sum256(block.Bytes)
	for _, protocol := range []string{"vless", "trojan", "http", "socks5"} {
		t.Run(protocol, func(t *testing.T) {
			endpoint := proxy.Proxy{Name: "pinned", Protocol: protocol, Server: "example.com", Port: 443, Security: "tls", TLS: &proxy.TLS{PinnedCA: string(ca)}, Credential: proxy.Credential{Username: "alice", Password: "secret"}}
			profile := format.Profile{Proxies: []proxy.Proxy{endpoint}}
			for _, exporter := range []format.Exporter{clash.Meta{}, singbox.Exporter{}, uri.URI{}, uri.Base64{}} {
				out, err := exporter.Render(profile)
				if err != nil {
					t.Fatal(err)
				}
				switch exporter.ID() {
				case "clash":
					if !strings.Contains(string(out.Body), hex.EncodeToString(digest[:])) {
						t.Fatalf("missing fingerprint: %s", out.Body)
					}
				case "sing-box":
					if !strings.Contains(string(out.Body), "certificate") {
						t.Fatalf("missing certificate: %s", out.Body)
					}
				default:
					if len(out.Body) != 0 || !strings.Contains(fmt.Sprint(out.Warnings), "自签名证书无法写入链接，请改用 Clash 或 sing-box 订阅") {
						t.Fatalf("unexpected output: %+v", out)
					}
				}
			}
		})
	}
}

func TestRelayHysteria2ClearsHopPorts(t *testing.T) {
	endpoint := proxy.Proxy{Name: "hy2", Protocol: "hysteria2", Server: "target.example", Port: 443, Security: "tls", TLS: &proxy.TLS{}, Credential: proxy.Credential{Password: "secret"}, QUIC: &proxy.QUIC{HopPorts: "20000-20010", HopInterval: 30, UpMbps: 10}}
	relay, ok := RelayEndpoint(endpoint, Relay{Server: "relay.example", Port: 8443, Network: "udp"})
	if !ok || relay.QUIC.HopPorts != "" || relay.QUIC.HopInterval != 0 || relay.QUIC.UpMbps != 10 || endpoint.QUIC.HopPorts != "20000-20010" || endpoint.QUIC.HopInterval != 30 {
		t.Fatal("incorrect QUIC copy")
	}
	for _, exporter := range []format.Exporter{clash.Meta{}, singbox.Exporter{}, uri.URI{}} {
		out, err := exporter.Render(format.Profile{Proxies: []proxy.Proxy{relay}})
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"ports:", "hop-interval", "server_ports", "hop_interval", "mport"} {
			if strings.Contains(string(out.Body), field) {
				t.Fatalf("%s contains %s: %s", exporter.ID(), field, out.Body)
			}
		}
	}
}

package subscription

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
)

const fixtureCA = "-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIUQ2VydGlmaWNhdGU=\n-----END CERTIFICATE-----"

// fixtureProxies is the pre-1.1 fixture set: every protocol the panel publishes,
// both stream and QUIC transports, a pinned CA and a WireGuard peer. The expected
// bytes in testdata were produced by the pre-refactor exporter, so the moved
// exporters are pinned against real output rather than against themselves.
func fixtureProxies(t *testing.T) []proxy.Proxy {
	t.Helper()
	seoul := func(inbound string) proxy.Origin {
		return proxy.Origin{Source: "published", Ref: inbound, Node: proxy.Ref{ID: "node-3", Name: "Seoul-01"}}
	}
	osaka := func(inbound string) proxy.Origin {
		return proxy.Origin{Source: "published", Ref: inbound, Node: proxy.Ref{ID: "node-2", Name: "Osaka-01"}}
	}
	tokyo := func(inbound string) proxy.Origin {
		return proxy.Origin{Source: "published", Ref: inbound, Node: proxy.Ref{ID: "node-1", Name: "Tokyo-01"}}
	}
	return []proxy.Proxy{
		{Origin: tokyo("in-vless-ws"), Name: "Tokyo-01 · vless", Server: "203.0.113.10", Port: 443, Protocol: "vless", Credential: proxy.Credential{Password: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}, Security: "tls", TLS: &proxy.TLS{ServerName: "cdn.example.com"}, Transport: &proxy.Transport{Type: "ws", Path: "/ray"}, Flow: "xtls-rprx-vision"},
		{Origin: tokyo("in-trojan-grpc"), Name: "Tokyo-01 · trojan", Server: "203.0.113.10", Port: 8443, Protocol: "trojan", Credential: proxy.Credential{Password: "trojan-secret"}, Security: "tls", TLS: &proxy.TLS{ServerName: "grpc.example.com"}, Transport: &proxy.Transport{Type: "grpc", ServiceName: "svc"}},
		{Origin: osaka("in-ss"), Name: "Osaka-01 · shadowsocks", Server: "198.51.100.7", Port: 8388, Protocol: "shadowsocks", Credential: proxy.Credential{Password: "ss-secret"}, Method: "aes-256-gcm"},
		{Origin: osaka("in-http"), Name: "Osaka-01 · http", Server: "198.51.100.7", Port: 8080, Protocol: "http", Credential: proxy.Credential{Username: "alice", Password: "http-pw"}},
		{Origin: osaka("in-socks"), Name: "Osaka-01 · socks5", Server: "198.51.100.7", Port: 1080, Protocol: "socks5", Credential: proxy.Credential{Username: "alice", Password: "socks-pw"}},
		{Origin: seoul("in-hy2"), Name: "Seoul-01 · hysteria2", Server: "2001:db8::1", Port: 443, Protocol: "hysteria2", Credential: proxy.Credential{Password: "hy2-secret"}, Security: "tls", TLS: &proxy.TLS{ServerName: "hy2.example.com"}, Transport: &proxy.Transport{Type: "quic"}, QUIC: &proxy.QUIC{UpMbps: 50, DownMbps: 100}},
		{Origin: seoul("in-tuic"), Name: "Seoul-01 · tuic", Server: "2001:db8::1", Port: 443, Protocol: "tuic", Credential: proxy.Credential{Username: "6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", Password: "tuic-secret"}, Security: "tls", TLS: &proxy.TLS{ServerName: "tuic.example.com"}, Transport: &proxy.Transport{Type: "quic"}, QUIC: &proxy.QUIC{CongestionControl: "bbr", UDPRelayMode: "quic"}},
		{Origin: seoul("in-hy2-pinned"), Name: "Seoul-01 · hysteria2", Server: "2001:db8::2", Port: 8443, Protocol: "hysteria2", Credential: proxy.Credential{Password: "pinned-secret"}, Security: "tls", TLS: &proxy.TLS{ServerName: "pinned.example.com", PinnedCA: fixtureCA}, Transport: &proxy.Transport{Type: "quic"}, QUIC: &proxy.QUIC{}},
		{Origin: seoul("in-tuic-pinned"), Name: "Seoul-01 · tuic", Server: "2001:db8::2", Port: 8444, Protocol: "tuic", Credential: proxy.Credential{Username: "7ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", Password: "pinned-tuic"}, Security: "tls", TLS: &proxy.TLS{ServerName: "pinned.example.com", PinnedCA: fixtureCA}, Transport: &proxy.Transport{Type: "quic"}},
		wireguardProxy(t),
	}
}

// wireguardProxy builds a WireGuard endpoint with a real X25519 key pair, so the
// exporter's identity checks pass.
func wireguardProxy(t *testing.T) proxy.Proxy {
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
	return proxy.Proxy{
		Origin:     proxy.Origin{Source: "published", Ref: "in-wg", Node: proxy.Ref{ID: "node-3", Name: "Seoul-01"}},
		Name:       "Seoul-01 · wireguard",
		Server:     "192.0.2.9",
		Port:       51820,
		Protocol:   "wireguard",
		Credential: proxy.Credential{Password: base64.StdEncoding.EncodeToString(private), PublicKey: base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())},
		WireGuard:  &proxy.WireGuard{ServerPublicKey: base64.StdEncoding.EncodeToString(server), Addresses: []string{"10.99.0.2/32", "2001:db8:99::2/128"}, MTU: 1420},
	}
}

func golden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A1.1: the URI, Base64 and sing-box formats are byte-identical to the
// pre-refactor exporter on the whole fixture set.
func TestSubscriptionFormatsMatchThePreRefactorFixture(t *testing.T) {
	profile := format.Profile{Proxies: fixtureProxies(t)}
	for _, testCase := range []struct {
		name     string
		exporter format.Exporter
		file     string
	}{
		{"uri", uri.URI{}, "legacy-uri.txt"},
		{"base64", uri.Base64{}, "legacy-base64.txt"},
		{"sing-box", singbox.Exporter{}, "legacy-sing-box.txt"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := testCase.exporter.Render(profile)
			if err != nil {
				t.Fatal(err)
			}
			if want := golden(t, testCase.file); !bytes.Equal(output.Body, want) {
				t.Fatalf("%s differs from the pre-refactor output\n--- got ---\n%s\n--- want ---\n%s", testCase.name, output.Body, want)
			}
		})
	}
}

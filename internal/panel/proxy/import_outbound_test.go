package proxy

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"reflect"
	"testing"

	subproxy "proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestImportOutboundConvertsFaithfully(t *testing.T) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	shortID, spider := "0123abcd", "/"
	uuid := "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"
	for _, tc := range []struct {
		name  string
		in    subproxy.Proxy
		check func(t *testing.T, out OutboundInput)
	}{
		{"vless reality vision", subproxy.Proxy{Protocol: "vless", Server: "23.246.167.253", Port: 36514, Flow: "xtls-rprx-vision", Security: "reality", Credential: subproxy.Credential{Password: uuid},
			TLS: &subproxy.TLS{ServerName: "www.example.com"}, Reality: &subproxy.RealityOptions{PublicKey: public, ShortID: &shortID, SpiderX: &spider}}, func(t *testing.T, out OutboundInput) {
			want := desiredstate.ProtocolSettings{Security: "reality", Flow: "xtls-rprx-vision", ServerName: "www.example.com", ClientFingerprint: "chrome", RealityClient: &desiredstate.RealityClientSettings{PublicKey: public, ShortID: shortID, SpiderX: spider}}
			if !reflect.DeepEqual(out.Settings, want) || out.Secret != uuid || out.Address != "23.246.167.253" || out.Port != 36514 || !out.SupportTCP || !out.SupportUDP {
				t.Fatalf("%+v", out)
			}
		}},
		{"vless tls xhttp with alpn filtering", subproxy.Proxy{Protocol: "vless", Server: "cdn.example.com", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: uuid},
			TLS: &subproxy.TLS{ServerName: "cdn.example.com", ALPN: []string{"h3", "h2"}, ClientFingerprint: "firefox"}, Transport: &subproxy.Transport{Type: "xhttp", XHTTP: &subproxy.XHTTPOptions{Host: "cdn.example.com", Path: "/x", Mode: "auto"}}}, func(t *testing.T, out OutboundInput) {
			s := out.Settings
			if s.Transport != "xhttp" || s.XHTTP == nil || s.XHTTP.Path != "/x" || !reflect.DeepEqual(s.ALPN, []string{"h2"}) || s.ClientFingerprint != "firefox" {
				t.Fatalf("%+v", s)
			}
		}},
		{"trojan ws host", subproxy.Proxy{Protocol: "trojan", Server: "t.example.com", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "secret"},
			TLS: &subproxy.TLS{ServerName: "t.example.com"}, Transport: &subproxy.Transport{Type: "ws", Path: "/ws", Host: "cdn.example.com"}}, func(t *testing.T, out OutboundInput) {
			if out.Settings.WSHost != "cdn.example.com" || out.Settings.Path != "/ws" || out.Secret != "secret" {
				t.Fatalf("%+v", out)
			}
		}},
		{"shadowsocks", subproxy.Proxy{Protocol: "shadowsocks", Server: "s.example.com", Port: 8388, Method: "aes-256-gcm", Credential: subproxy.Credential{Password: "pw"}}, func(t *testing.T, out OutboundInput) {
			if out.Settings.Method != "aes-256-gcm" || out.Secret != "pw" || !out.SupportUDP {
				t.Fatalf("%+v", out)
			}
		}},
		{"socks5", subproxy.Proxy{Protocol: "socks5", Server: "198.51.100.1", Port: 1080, Credential: subproxy.Credential{Username: "u", Password: "p"}}, func(t *testing.T, out OutboundInput) {
			if out.Username != "u" || out.Secret != "p" || !out.SupportUDP {
				t.Fatalf("%+v", out)
			}
		}},
		{"tuic uuid and password", subproxy.Proxy{Protocol: "tuic", Server: "q.example.com", Port: 443, Security: "tls", Credential: subproxy.Credential{Username: uuid, Password: "pw"},
			TLS: &subproxy.TLS{ServerName: "q.example.com"}, QUIC: &subproxy.QUIC{CongestionControl: "bbr"}}, func(t *testing.T, out OutboundInput) {
			if out.Username != uuid || out.Secret != "pw" || !out.SupportTCP || !out.SupportUDP || out.Settings.QUIC == nil || out.Settings.QUIC.CongestionControl != "bbr" {
				t.Fatalf("%+v", out)
			}
		}},
		{"hysteria2 hop ports dropped", subproxy.Proxy{Protocol: "hysteria2", Server: "h.example.com", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "pw"},
			TLS: &subproxy.TLS{ServerName: "h.example.com"}, QUIC: &subproxy.QUIC{HopPorts: "20000-20010", HopInterval: 30}}, func(t *testing.T, out OutboundInput) {
			if out.Port != 443 || out.Settings.QUIC != nil {
				t.Fatalf("%+v", out)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := ImportOutbound(tc.in)
			if code != "" {
				t.Fatalf("refused: %s", code)
			}
			tc.check(t, out)
		})
	}
}

func TestImportOutboundRefusesLossyConversions(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   subproxy.Proxy
		want string
	}{
		{"wireguard", subproxy.Proxy{Protocol: "wireguard", Server: "w.example.com", Port: 51820}, importUnsupportedProtocol},
		{"insecure", subproxy.Proxy{Protocol: "trojan", Server: "t.example.com", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "p"}, TLS: &subproxy.TLS{Insecure: true}}, importInsecureTLS},
		{"certificate pin", subproxy.Proxy{Protocol: "trojan", Server: "t.example.com", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "p"}, TLS: &subproxy.TLS{Fingerprint: "ab"}}, importCertificatePin},
		{"ss2022", subproxy.Proxy{Protocol: "shadowsocks", Server: "s.example.com", Port: 8388, Method: "2022-blake3-aes-128-gcm", Credential: subproxy.Credential{Password: "k"}}, importUnsupportedCipher},
		{"hysteria2 obfs", subproxy.Proxy{Protocol: "hysteria2", Server: "h.example.com", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "p"}, TLS: &subproxy.TLS{ServerName: "h.example.com"}, QUIC: &subproxy.QUIC{Obfs: "salamander", ObfsPassword: "x"}}, importUnsupportedQUIC},
		{"http upgrade", subproxy.Proxy{Protocol: "vless", Server: "v.example.com", Port: 443, Security: "tls", Credential: subproxy.Credential{Password: "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"}, TLS: &subproxy.TLS{ServerName: "v.example.com"}, Transport: &subproxy.Transport{Type: "httpupgrade"}}, importUnsupportedTransport},
		{"bad reality key", subproxy.Proxy{Protocol: "vless", Server: "v.example.com", Port: 443, Security: "reality", Credential: subproxy.Credential{Password: "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"}, TLS: &subproxy.TLS{ServerName: "www.example.com"}, Reality: &subproxy.RealityOptions{PublicKey: "AAAA"}}, importInvalidSettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, code := ImportOutbound(tc.in); code != tc.want {
				t.Fatalf("got %q, want %q", code, tc.want)
			}
			if ImportOutboundReason(tc.want) == "" {
				t.Fatal("missing Chinese reason")
			}
		})
	}
}

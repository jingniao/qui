package proxy

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"proxycontrol/pkg/contracts/netaddr"
	"strconv"
	"strings"
	"unicode"
)

// ValidateClientSettings validates public connection settings without depending
// on panel or Agent models. It returns only fixed safe codes and field paths.
func ValidateClientSettings(p Proxy) (string, string) {
	if q := p.QUIC; q != nil {
		if (q.Obfs != "" || q.ObfsPassword != "") && p.Protocol != "hysteria2" {
			return "invalid_protocol_combination", "entry"
		}
		if q.Obfs != "" && q.Obfs != "salamander" || q.Obfs == "" && q.ObfsPassword != "" || q.Obfs != "" && (q.ObfsPassword == "" || len(q.ObfsPassword) > 256 || strings.IndexFunc(q.ObfsPassword, unicode.IsControl) >= 0) {
			return "unsupported_option", "entry"
		}
		if p.Protocol != "tuic" && p.Protocol != "hysteria2" {
			return "invalid_protocol_combination", "entry"
		}
		if q.CongestionControl != "" && q.CongestionControl != "bbr" && q.CongestionControl != "cubic" && q.CongestionControl != "new_reno" {
			return "unsupported_option", "entry"
		}
		if q.UDPRelayMode != "" && q.UDPRelayMode != "native" && q.UDPRelayMode != "quic" {
			return "unsupported_option", "entry"
		}
		if q.UpMbps < 0 || q.DownMbps < 0 || q.HopInterval < 0 {
			return "unsupported_option", "entry"
		}
		if p.Protocol == "tuic" && (q.HopPorts != "" || q.HopInterval != 0 || q.UpMbps != 0 || q.DownMbps != 0) || p.Protocol == "hysteria2" && (q.CongestionControl != "" || q.UDPRelayMode != "") {
			return "invalid_protocol_combination", "entry"
		}
		if q.HopPorts != "" {
			parts := strings.Split(q.HopPorts, "-")
			if len(parts) != 2 {
				return "unsupported_option", "entry"
			}
			start, e1 := strconv.Atoi(parts[0])
			end, e2 := strconv.Atoi(parts[1])
			if e1 != nil || e2 != nil || start < 1 || end > 65535 || start >= end || strconv.Itoa(start) != parts[0] || strconv.Itoa(end) != parts[1] {
				return "unsupported_option", "entry"
			}
		}
	}
	if p.Transport != nil && p.Transport.Host != "" {
		if p.Transport.Type != "ws" {
			return "invalid_protocol_combination", "transport.host"
		}
		if len(p.Transport.Host) > 253 || !netaddr.ValidHostName(p.Transport.Host) {
			return "invalid_endpoint", "transport.host"
		}
	}
	if p.Protocol == "vless" && p.Flow != "" && p.Transport != nil && (p.Transport.Type == "grpc" || p.Transport.Type == "xhttp") {
		return "invalid_protocol_combination", "flow"
	}
	if p.TLS != nil && p.TLS.Fingerprint != "" {
		pin, err := hex.DecodeString(strings.ReplaceAll(p.TLS.Fingerprint, ":", ""))
		if err != nil || len(pin) != 32 {
			return "invalid_fingerprint", "tls.certificateFingerprint"
		}
	}
	if p.Transport != nil && p.Transport.XHTTP != nil && p.Transport.Type != "xhttp" {
		return "invalid_protocol_combination", "transport"
	}
	if p.Transport != nil && p.Transport.Type == "xhttp" {
		if p.TLS != nil {
			for _, alpn := range p.TLS.ALPN {
				if alpn != "h2" && alpn != "http/1.1" {
					return "invalid_protocol_combination", "tls.alpn"
				}
			}
		}
		x := p.Transport.XHTTP
		if p.Protocol != "vless" || (p.Security != "tls" && p.Security != "reality") || x == nil || p.Transport.Path != "" || p.Transport.ServiceName != "" {
			return "invalid_xhttp_settings", "transport.xhttp"
		}
		if p.Flow != "" {
			return "invalid_protocol_combination", "flow"
		}
		switch x.Mode {
		case "auto", "packet-up", "stream-up", "stream-one":
		default:
			return "unsupported_xhttp_mode", "transport.xhttp.mode"
		}
		if len(x.Host) > 253 || strings.ContainsAny(x.Host, " /\\\r\n\t@") || strings.IndexFunc(x.Host, unicode.IsControl) >= 0 {
			return "invalid_xhttp_settings", "transport.xhttp.host"
		}
		if len(x.Path) > 2048 || (x.Path != "" && !strings.HasPrefix(x.Path, "/")) || strings.IndexFunc(x.Path, unicode.IsControl) >= 0 {
			return "invalid_xhttp_settings", "transport.xhttp.path"
		}
	}
	if p.TLS != nil && p.TLS.ClientFingerprint != "" {
		if (p.Security != "tls" && p.Security != "reality") || (p.Protocol != "vless" && p.Protocol != "trojan") {
			return "invalid_protocol_combination", "tls.clientFingerprint"
		}
		switch p.TLS.ClientFingerprint {
		case "chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized":
		default:
			return "invalid_fingerprint", "tls.clientFingerprint"
		}
	}
	if p.Security != "reality" {
		if p.Reality != nil {
			return "invalid_protocol_combination", "reality"
		}
		return "", ""
	}
	if p.Protocol != "vless" || p.TLS == nil || p.Reality == nil {
		return "invalid_reality_settings", "reality"
	}
	identity := p.Credential.Password
	if len(identity) != 36 || identity[8] != '-' || identity[13] != '-' || identity[18] != '-' || identity[23] != '-' {
		return "invalid_uuid", "credential"
	}
	if decoded, err := hex.DecodeString(strings.ReplaceAll(identity, "-", "")); err != nil || len(decoded) != 16 {
		return "invalid_uuid", "credential"
	}
	if p.TLS.Insecure || p.TLS.PinnedCA != "" || p.TLS.Fingerprint != "" {
		return "invalid_protocol_combination", "reality"
	}
	if p.TLS.ServerName == "" || len(p.TLS.ServerName) > 253 || strings.ContainsAny(p.TLS.ServerName, " /\\\r\n\t@:") || strings.IndexFunc(p.TLS.ServerName, unicode.IsControl) >= 0 {
		return "invalid_reality_settings", "tls.serverName"
	}
	if p.TLS.ClientFingerprint == "" {
		return "invalid_fingerprint", "tls.clientFingerprint"
	}
	r := p.Reality
	key, err := base64.RawURLEncoding.DecodeString(r.PublicKey)
	if err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != r.PublicKey {
		return "invalid_reality_key", "reality.publicKey"
	}
	// Reject low-order keys too: a syntactically valid all-zero public key cannot
	// complete an X25519 handshake.
	pub, err := ecdh.X25519().NewPublicKey(key)
	private, _ := ecdh.X25519().NewPrivateKey([]byte("01234567890123456789012345678901"))
	if err != nil {
		return "invalid_reality_key", "reality.publicKey"
	}
	if _, err = private.ECDH(pub); err != nil {
		return "invalid_reality_key", "reality.publicKey"
	}
	if r.ShortID == nil {
		return "invalid_reality_short_id", "reality.shortId"
	}
	if len(*r.ShortID) > 16 || len(*r.ShortID)%2 != 0 {
		return "invalid_reality_short_id", "reality.shortId"
	}
	if _, err := hex.DecodeString(*r.ShortID); err != nil {
		return "invalid_reality_short_id", "reality.shortId"
	}
	if r.SpiderX != nil && (len(*r.SpiderX) > 2048 || strings.IndexFunc(*r.SpiderX, unicode.IsControl) >= 0) {
		return "invalid_reality_settings", "reality.spiderX"
	}
	transport := "tcp"
	if p.Transport != nil && p.Transport.Type != "" {
		transport = p.Transport.Type
	}
	if transport != "tcp" && transport != "grpc" && transport != "xhttp" {
		return "invalid_protocol_combination", "transport"
	}
	if p.Transport != nil && transport != "xhttp" && p.Transport.Path != "" {
		return "invalid_protocol_combination", "transport"
	}
	if p.Transport != nil && transport != "grpc" && p.Transport.ServiceName != "" {
		return "invalid_protocol_combination", "transport"
	}
	if p.Flow != "" && (p.Flow != "xtls-rprx-vision" || transport != "tcp") {
		return "invalid_protocol_combination", "flow"
	}
	return "", ""
}

// ClientFormatSupportsSettings is the measured capability boundary. Valid public
// settings may still be unrepresentable in a selected client format.
func ClientFormatSupportsSettings(p Proxy, format string) bool {
	return ClientFormatOmission(p, format) == ""
}

// ClientFormatOmission returns a fixed reason at the measured capability boundary.
func ClientFormatOmission(p Proxy, format string) string {
	if supportsClientSettings(p, format) {
		return ""
	}
	if p.Transport != nil && p.Transport.Type == "xhttp" {
		if format != "clash" && p.Transport.XHTTP != nil && p.Transport.XHTTP.Mode != "auto" {
			return "xhttp_mode"
		}
		if p.TLS != nil && len(p.TLS.ALPN) != 0 && (len(p.TLS.ALPN) != 1 || p.TLS.ALPN[0] != "h2") {
			return "xhttp_alpn"
		}
	}
	if p.Security == "reality" {
		if p.Transport != nil && p.Transport.Type == "grpc" {
			return "reality_grpc"
		}
		if p.Flow != "xtls-rprx-vision" {
			return "reality_no_vision"
		}
	}
	return "unsupported_settings"
}

func supportsClientSettings(p Proxy, format string) bool {
	if p.Transport != nil && p.Transport.Type == "xhttp" {
		if format == "uri" || format == "base64" {
			return true
		}
		if format != "clash" || (p.Security != "reality" && p.Security != "tls") || p.Transport.XHTTP == nil {
			return false
		}
		mode := p.Transport.XHTTP.Mode
		return (mode == "auto" || mode == "stream-one" || mode == "packet-up" || mode == "stream-up") && (p.TLS == nil || len(p.TLS.ALPN) == 0 || len(p.TLS.ALPN) == 1 && p.TLS.ALPN[0] == "h2")
	}
	if p.Security != "reality" {
		return true
	}
	if format == "uri" || format == "base64" {
		return p.Transport == nil || p.Transport.Type == "" || p.Transport.Type == "tcp" || p.Transport.Type == "grpc"
	}
	if format != "clash" && format != "sing-box" {
		return false
	}
	if format == "clash" {
		return p.Reality != nil && (p.Transport == nil || p.Transport.Type == "" || p.Transport.Type == "tcp" || p.Transport.Type == "grpc")
	}
	return p.Flow == "xtls-rprx-vision" && (p.Transport == nil || p.Transport.Type == "" || p.Transport.Type == "tcp") && p.Reality != nil
}

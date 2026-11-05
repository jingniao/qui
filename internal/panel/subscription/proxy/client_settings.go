package proxy

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"unicode"
)

// ValidateClientSettings validates public connection settings without depending
// on panel or Agent models. It returns only fixed safe codes and field paths.
func ValidateClientSettings(p Proxy) (string, string) {
	if p.TLS != nil && p.TLS.Fingerprint != "" {
		pin, err := hex.DecodeString(strings.ReplaceAll(p.TLS.Fingerprint, ":", ""))
		if err != nil || len(pin) != 32 {
			return "invalid_fingerprint", "tls.certificateFingerprint"
		}
	}
	if p.Transport != nil && p.Transport.XHTTP != nil && p.Transport.Type != "xhttp" {
		return "invalid_protocol_combination", "transport"
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
	if p.Security != "reality" {
		return true
	}
	if format == "uri" || format == "base64" {
		return p.Transport == nil || p.Transport.Type == "" || p.Transport.Type == "tcp" || p.Transport.Type == "grpc"
	}
	if format != "clash" && format != "sing-box" {
		return false
	}
	return p.Flow == "xtls-rprx-vision" && (p.Transport == nil || p.Transport.Type == "" || p.Transport.Type == "tcp") && p.Reality != nil && p.Reality.SpiderX == nil
}

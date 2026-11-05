package proxy

import (
	"encoding/base64"
	"strings"
)

// ShadowsocksError is a safe, fixed diagnostic code; it never contains key data.
type ShadowsocksError string

func (e ShadowsocksError) Error() string { return string(e) }

const (
	ErrSSCipher      ShadowsocksError = "unsupported_cipher"
	ErrSSKeyEncoding ShadowsocksError = "invalid_ss2022_key_encoding"
	ErrSSKeyLength   ShadowsocksError = "invalid_ss2022_key_length"
	ErrSSIdentity    ShadowsocksError = "unsupported_ss2022_identity"
	ErrSSSecurity    ShadowsocksError = "unsupported_security"
	ErrSSTransport   ShadowsocksError = "unsupported_transport"
	ErrSSOption      ShadowsocksError = "unsupported_option"
)

// SS2022 reports whether method names an AEAD-2022 cipher, including unknown
// ciphers that must fail closed rather than falling through the legacy path.
func SS2022(method string) bool { return strings.HasPrefix(strings.ToLower(method), "2022-") }

// ShadowsocksCredential is the validated shape of a client credential. Keys stay
// in Credential.Password for encrypted serialization; no second secret copy is
// added to the neutral model. Identity means serverPSK:userPSK, in that order.
type ShadowsocksCredential struct {
	Method   string
	Identity bool
}

// Shadowsocks validates the endpoint as well as the cipher and keys. AEAD-2022
// imports in this release have no TLS wrapper, stream transport or VLESS flow;
// accepting such settings would lose connection semantics in some exporters.
func (p Proxy) Shadowsocks() (ShadowsocksCredential, error) {
	c, err := ValidateShadowsocks(p.Method, p.Credential.Password)
	if err != nil || !SS2022(p.Method) {
		return c, err
	}
	if p.Security != "" && p.Security != "none" || p.TLS != nil {
		return c, ErrSSSecurity
	}
	if p.Transport != nil && (p.Transport.Type != "" && p.Transport.Type != "tcp" || p.Transport.Path != "" || p.Transport.ServiceName != "") {
		return c, ErrSSTransport
	}
	if p.Flow != "" || p.QUIC != nil || p.WireGuard != nil || p.Credential.Username != "" || p.Credential.PublicKey != "" {
		return c, ErrSSOption
	}
	return c, nil
}

// ValidateShadowsocks is shared by import, archive verification and export. The
// measured initial subset permits exactly one key or two AES identity keys;
// arbitrary relay/identity chains have not been validated against target clients.
func ValidateShadowsocks(method, password string) (ShadowsocksCredential, error) {
	c := ShadowsocksCredential{Method: method}
	size := 0
	switch method {
	case "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305":
		return c, nil
	case "2022-blake3-aes-128-gcm":
		size = 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		size = 32
	default:
		return c, ErrSSCipher
	}
	parts := strings.Split(password, ":")
	if len(parts) > 2 || (len(parts) > 1 && method == "2022-blake3-chacha20-poly1305") {
		return c, ErrSSIdentity
	}
	for _, part := range parts {
		key, err := base64.StdEncoding.Strict().DecodeString(part)
		canonical := base64.StdEncoding.EncodeToString(key)
		length := len(key)
		clear(key)
		if err != nil || canonical != part {
			return c, ErrSSKeyEncoding
		}
		if length != size {
			return c, ErrSSKeyLength
		}
	}
	c.Identity = len(parts) == 2
	return c, nil
}

// Omission is the pinned measured format matrix (v1.4-s0-evidence.md §8).
// Empty means representable. Validation errors are separate: corrupted data must
// not be mistaken for a normal client capability omission.
func (c ShadowsocksCredential) Omission(format string) string {
	if !SS2022(c.Method) {
		return ""
	}
	switch format {
	case "uri", "base64":
		return ""
	case "clash":
		if c.Method != "2022-blake3-aes-128-gcm" || !c.Identity {
			return ""
		}
	case "sing-box":
		if c.Method != "2022-blake3-chacha20-poly1305" {
			return ""
		}
	}
	return "SS2022 cipher/identity combination is unverified for this client format"
}

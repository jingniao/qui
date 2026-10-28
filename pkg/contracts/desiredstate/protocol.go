package desiredstate

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/netip"
	"strings"
)

func ValidateSettings(protocol string, settings ProtocolSettings, inbound bool) error {
	if protocol == "forward" {
		f := settings.Forward
		if !inbound || f == nil || !validForwardHost(f.Host) || f.Port < 1 || f.Port > 65535 || (f.Network != "tcp" && f.Network != "udp" && f.Network != "tcp,udp") {
			return errors.New("forward requires a target host, port and tcp/udp network")
		}
		if settings.Transport != "" || settings.Security != "" || settings.WireGuard != nil || settings.QUIC != nil || settings.CertificatePEM != "" || settings.PrivateKeyPEM != "" || settings.TrustedCAPEM != "" || settings.Method != "" || settings.Flow != "" || settings.Path != "" || settings.ServiceName != "" || settings.ServerName != "" {
			return errors.New("forward cannot carry proxy security or transport settings")
		}
		return nil
	}
	if settings.Forward != nil {
		return errors.New("forward settings require a forward inbound")
	}
	quic := protocol == "hysteria2" || protocol == "tuic"
	if settings.QUIC != nil && !quic {
		return errors.New("QUIC settings require Hysteria2 or TUIC")
	}
	if quic {
		if settings.Security != "tls" || settings.Transport != "" && settings.Transport != "quic" || settings.WireGuard != nil || settings.Method != "" || settings.Flow != "" || settings.Path != "" || settings.ServiceName != "" {
			return errors.New("Hysteria2/TUIC require native QUIC and TLS without legacy transport fields")
		}
		if q := settings.QUIC; q != nil {
			if q.UpMbps < 0 || q.DownMbps < 0 || q.UpMbps > 100000 || q.DownMbps > 100000 {
				return errors.New("QUIC bandwidth is outside allowed range")
			}
			if protocol == "hysteria2" && (q.CongestionControl != "" || q.UDPRelayMode != "") {
				return errors.New("Hysteria2 cannot use TUIC options")
			}
			if protocol == "tuic" {
				if q.UpMbps != 0 || q.DownMbps != 0 {
					return errors.New("TUIC cannot use Hysteria2 bandwidth options")
				}
				if q.CongestionControl != "" && !strings.Contains("|cubic|new_reno|bbr|", "|"+q.CongestionControl+"|") {
					return errors.New("invalid TUIC congestion control")
				}
				if q.UDPRelayMode != "" && q.UDPRelayMode != "native" && q.UDPRelayMode != "quic" {
					return errors.New("invalid TUIC UDP relay mode")
				}
				if inbound && q.UDPRelayMode != "" {
					return errors.New("UDP relay mode is a TUIC client option")
				}
			}
		}
	}
	if protocol == "direct" || protocol == "block" {
		if settings.Security != "" || settings.Transport != "" || settings.WireGuard != nil {
			return errors.New("direct/block cannot use transport security")
		}
		return nil
	}
	if settings.Transport != "" && settings.Transport != "tcp" && settings.Transport != "ws" && settings.Transport != "grpc" && !(quic && settings.Transport == "quic") {
		return errors.New("unsupported proxy transport")
	}
	if settings.Security != "" && settings.Security != "none" && settings.Security != "tls" {
		return errors.New("unsupported transport security")
	}
	if len(settings.Path) > 256 || len(settings.ServiceName) > 128 || len(settings.ServerName) > 253 {
		return errors.New("transport field too long")
	}
	if settings.Path != "" && !strings.HasPrefix(settings.Path, "/") {
		return errors.New("WebSocket path must begin with slash")
	}
	if settings.Flow != "" && (protocol != "vless" || settings.Flow != "xtls-rprx-vision" || settings.Security != "tls" || settings.Transport != "" && settings.Transport != "tcp") {
		return errors.New("VLESS flow requires TCP TLS vision")
	}
	if protocol == "trojan" && settings.Security != "tls" {
		return errors.New("Trojan requires TLS")
	}
	if settings.Security == "tls" && inbound {
		if len(settings.CertificatePEM) > 32768 || len(settings.PrivateKeyPEM) > 16384 {
			return errors.New("TLS material exceeds limit")
		}
		if _, err := tls.X509KeyPair([]byte(settings.CertificatePEM), []byte(settings.PrivateKeyPEM)); err != nil {
			return errors.New("valid TLS certificate and matching key are required")
		}
	}
	if settings.TrustedCAPEM != "" {
		if len(settings.TrustedCAPEM) > 32768 {
			return errors.New("TLS CA material exceeds limit")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(settings.TrustedCAPEM)) {
			return errors.New("invalid trusted CA certificate")
		}
	}
	if settings.PrivateKeyPEM != "" && settings.Security != "tls" {
		return errors.New("TLS key cannot be used without TLS")
	}
	if protocol == "shadowsocks" {
		method := settings.Method
		if method == "" {
			method = "chacha20-ietf-poly1305"
		}
		if !strings.Contains("|aes-128-gcm|aes-256-gcm|chacha20-ietf-poly1305|xchacha20-ietf-poly1305|", "|"+method+"|") {
			return errors.New("unsupported Shadowsocks cipher")
		}
	}
	if protocol == "wireguard" {
		if settings.Transport != "" || settings.Security != "" && settings.Security != "none" {
			return errors.New("WireGuard uses its own UDP transport security")
		}
		wireguard := settings.WireGuard
		if wireguard == nil {
			return errors.New("WireGuard settings required")
		}
		key, err := base64.StdEncoding.DecodeString(wireguard.SecretKey)
		if err != nil || len(key) != 32 {
			return errors.New("invalid WireGuard private key")
		}
		if wireguard.MTU != 0 && (wireguard.MTU < 576 || wireguard.MTU > 9000) {
			return errors.New("invalid WireGuard MTU")
		}
		if len(wireguard.Addresses) == 0 || len(wireguard.Addresses) > 8 || len(wireguard.Peers) > 256 {
			return errors.New("invalid WireGuard address or peer count")
		}
		for _, address := range wireguard.Addresses {
			if _, err := netip.ParsePrefix(address); err != nil {
				if _, err := netip.ParseAddr(address); err != nil {
					return errors.New("invalid WireGuard interface address")
				}
			}
		}
		for _, peer := range wireguard.Peers {
			key, err := base64.StdEncoding.DecodeString(peer.PublicKey)
			if err != nil || len(key) != 32 {
				return errors.New("invalid WireGuard peer key")
			}
			if peer.KeepAlive < 0 || peer.KeepAlive > 3600 {
				return errors.New("invalid WireGuard keepalive")
			}
			for _, prefix := range peer.AllowedIPs {
				if _, err := netip.ParsePrefix(prefix); err != nil {
					return errors.New("invalid WireGuard allowed prefix")
				}
			}
		}
	}
	if settings.CertificatePEM != "" {
		block, _ := pem.Decode([]byte(settings.CertificatePEM))
		if block == nil || block.Type != "CERTIFICATE" {
			return errors.New("invalid TLS certificate PEM")
		}
	}
	return nil
}

func validForwardHost(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return !ip.IsUnspecified() && !ip.IsMulticast() && ip.Zone() == ""
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, part := range strings.Split(host, ".") {
		if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func ValidateUUID(value string) error {
	if len(value) != 36 {
		return errors.New("TUIC requires a UUID identity")
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return errors.New("invalid UUID identity")
			}
			continue
		}
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return errors.New("invalid UUID identity")
		}
	}
	return nil
}
func ValidateWireGuardPublicKey(value string) error {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != 32 {
		return errors.New("invalid WireGuard public key")
	}
	return nil
}

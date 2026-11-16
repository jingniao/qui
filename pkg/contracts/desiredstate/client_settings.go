package desiredstate

import (
	"errors"
	"strings"
	"unicode"

	"proxycontrol/pkg/contracts/clienttls"
	"proxycontrol/pkg/contracts/netaddr"
)

// Client-side settings an outbound dials with (v1.6.0 plan section 1). They let a
// server reach an external endpoint, such as an imported node, with the same TLS
// details a client would use. They are outbound-only: an inbound presents
// certificates and Reality's server half instead.

// RealityClientSettings is the client half of VLESS Reality. The SNI is the
// outbound's server_name and the uTLS fingerprint its client_fingerprint.
type RealityClientSettings struct {
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id,omitempty"`
	SpiderX   string `json:"spider_x,omitempty"`
}

const (
	maxClientALPN    = 3
	maxSpiderXLength = 2048
)

// HasClientSettings reports whether a settings block uses any outbound client
// field. Old Agents refuse these unknown fields, so the panel gates them on the
// Agent's outbound settings report (OutboundSettingsCombination).
func (s ProtocolSettings) HasClientSettings() bool {
	return len(s.ALPN) > 0 || s.ClientFingerprint != "" || s.WSHost != "" || s.RealityClient != nil
}

func validateClientSettings(protocol string, settings ProtocolSettings, inbound bool) error {
	if !settings.HasClientSettings() {
		return nil
	}
	if inbound {
		return errors.New("ALPN, client fingerprint, WebSocket host and Reality client settings require an outbound")
	}
	secured := settings.Security == "tls" || settings.Security == SecurityReality
	// Hysteria2 and TUIC always negotiate h3, which the renderers set themselves.
	if len(settings.ALPN) > 0 {
		if (protocol != "vless" && protocol != "trojan") || !secured {
			return errors.New("ALPN requires VLESS or Trojan with TLS or Reality")
		}
		if len(settings.ALPN) > maxClientALPN {
			return errors.New("at most three ALPN values")
		}
		seen := map[string]bool{}
		for _, value := range settings.ALPN {
			if seen[value] || value != "h2" && value != "http/1.1" {
				return errors.New("ALPN values must be distinct h2 or http/1.1")
			}
			seen[value] = true
		}
	}
	if settings.ClientFingerprint != "" {
		if (protocol != "vless" && protocol != "trojan") || !secured {
			return errors.New("client fingerprint requires VLESS or Trojan with TLS or Reality")
		}
		if !clienttls.ValidFingerprint(settings.ClientFingerprint) {
			return errors.New("unsupported client fingerprint")
		}
	}
	if settings.WSHost != "" && (settings.Transport != "ws" || !netaddr.ValidHostName(settings.WSHost)) {
		return errors.New("WebSocket host requires the ws transport and a host name")
	}
	if r := settings.RealityClient; r != nil {
		if protocol != "vless" || settings.Security != SecurityReality {
			return errors.New("reality client settings require a VLESS outbound with reality security")
		}
		if settings.CertificatePEM != "" || settings.PrivateKeyPEM != "" || settings.TrustedCAPEM != "" {
			return errors.New("reality cannot carry TLS certificate material")
		}
		if !validServerName(settings.ServerName) {
			return errors.New("reality requires a concrete server name")
		}
		if settings.ClientFingerprint == "" {
			return errors.New("reality requires a client fingerprint")
		}
		if err := clienttls.ValidRealityPublicKey(r.PublicKey); err != nil {
			return err
		}
		if r.ShortID != "" && !validShortID(r.ShortID) {
			return errors.New("invalid reality short id")
		}
		if len(r.SpiderX) > maxSpiderXLength || strings.IndexFunc(r.SpiderX, unicode.IsControl) >= 0 {
			return errors.New("invalid reality spider path")
		}
	}
	return nil
}

// OutboundClientCombinations are every outbound shape the client fields can
// appear in. Each core adapter probes the subset its renderer supports
// (ValidateCoreClientSettings) and reports what passed its native check.
func OutboundClientCombinations() []SettingsCombination {
	shapes := [][4]string{
		{"vless", "none", "ws", ""},
		{"vless", "tls", "tcp", ""}, {"vless", "tls", "tcp", "xtls-rprx-vision"}, {"vless", "tls", "ws", ""}, {"vless", "tls", "grpc", ""}, {"vless", "tls", TransportXHTTP, ""},
		{"vless", SecurityReality, "tcp", ""}, {"vless", SecurityReality, "tcp", "xtls-rprx-vision"}, {"vless", SecurityReality, "grpc", ""}, {"vless", SecurityReality, TransportXHTTP, ""},
		{"trojan", "tls", "tcp", ""}, {"trojan", "tls", "ws", ""}, {"trojan", "tls", "grpc", ""},
	}
	result := make([]SettingsCombination, 0, len(shapes))
	for _, shape := range shapes {
		result = append(result, SettingsCombination{Protocol: shape[0], Direction: DirectionOutbound, Security: shape[1], Transport: shape[2], Flow: shape[3]})
	}
	return result
}

// OutboundSettingsCombination derives the combination one outbound requires. Only
// an outbound using the client fields or the xhttp transport needs a report; every
// legacy outbound renders on any Agent, as before v1.6.0.
func OutboundSettingsCombination(protocol string, settings ProtocolSettings) (SettingsCombination, bool) {
	if !settings.HasClientSettings() && settings.XHTTP == nil {
		return SettingsCombination{}, false
	}
	combination := SettingsCombination{Protocol: protocol, Direction: DirectionOutbound, Security: settings.Security, Transport: settings.Transport, Flow: settings.Flow}
	if combination.Security == "" {
		combination.Security = "none"
	}
	if combination.Transport == "" {
		combination.Transport = "tcp"
	}
	return combination, true
}

// RequireOutboundSettings is the outbound counterpart of
// RequireInboundServerSettings: an old Agent refuses the client fields as unknown,
// so every layer must find the exact combination in the core's report first.
func RequireOutboundSettings(core string, capability CoreSettingsCapability, protocol string, settings ProtocolSettings) error {
	if err := ValidateCoreClientSettings(core, settings); err != nil {
		return err
	}
	combination, required := OutboundSettingsCombination(protocol, settings)
	if !required {
		return nil
	}
	if err := RequireSettingsCapability(core, capability, combination); err != nil {
		return errors.New("outbound client settings are not available on this node yet: " + err.Error())
	}
	return nil
}

// ValidateCoreClientSettings keeps an outbound within what a core's client model
// can express: sing-box has no xhttp and dials Reality over TCP only.
func ValidateCoreClientSettings(core string, settings ProtocolSettings) error {
	if core != CoreSingBox {
		return nil
	}
	if settings.Transport == TransportXHTTP {
		return errors.New("sing-box does not support xhttp")
	}
	if settings.Security == SecurityReality && settings.Transport != "" && settings.Transport != "tcp" {
		return errors.New("sing-box Reality currently requires TCP")
	}
	return nil
}

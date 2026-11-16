package proxy

import (
	subproxy "proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Fixed reasons an imported endpoint has no faithful server-side outbound
// (v1.6.0 plan 1.3). The canvas hides such endpoints in server schemes.
const (
	importUnsupportedProtocol  = "import_protocol_unsupported"
	importInsecureTLS          = "import_tls_insecure"
	importCertificatePin       = "import_certificate_pin"
	importUnsupportedTransport = "import_transport_unsupported"
	importUnsupportedCipher    = "import_cipher_unsupported"
	importUnsupportedQUIC      = "import_quic_option_unsupported"
	importInvalidSettings      = "import_settings_invalid"
)

// importOutboundReasons are the Chinese texts the canvas and quick flow show.
var importOutboundReasons = map[string]string{
	importUnsupportedProtocol:  "服务器出站不支持该协议",
	importInsecureTLS:          "导入节点跳过了证书校验，服务器出站不静默跳过校验",
	importCertificatePin:       "导入节点固定了证书指纹，服务器出站暂不支持证书指纹",
	importUnsupportedTransport: "服务器出站不支持该传输方式",
	importUnsupportedCipher:    "服务器出站不支持该加密方式（如 Shadowsocks 2022）",
	importUnsupportedQUIC:      "服务器出站不支持该 QUIC 选项（如混淆）",
	importInvalidSettings:      "导入节点的参数无法转换为服务器出站",
}

// ImportOutboundReason is the fixed Chinese text for an ImportOutbound code.
func ImportOutboundReason(code string) string { return importOutboundReasons[code] }

// ImportOutbound converts an imported endpoint into the outbound a server dials
// it with: protocol, address, credential and the client TLS details. It leaves
// Name and NodeID to the caller. The code is "" on success, otherwise a fixed
// reason: an endpoint is never converted lossily. Hysteria2 hop ports are
// dropped and the main port dialled, as for relayed subscription endpoints.
func ImportOutbound(p subproxy.Proxy) (OutboundInput, string) {
	input := OutboundInput{Protocol: p.Protocol, Address: p.Server, Port: p.Port, Enabled: true, FailurePolicy: "Block", SupportTCP: true}
	settings := desiredstate.ProtocolSettings{Security: p.Security, Flow: p.Flow}
	switch p.Protocol {
	case "socks5", "http":
		input.Username, input.Secret = p.Credential.Username, p.Credential.Password
		input.SupportUDP = p.Protocol == "socks5"
	case "shadowsocks":
		if subproxy.SS2022(p.Method) {
			return input, importUnsupportedCipher
		}
		settings.Method, input.Secret, input.SupportUDP = p.Method, p.Credential.Password, true
	case "vless", "trojan", "hysteria2":
		input.Secret, input.SupportUDP = p.Credential.Password, true
	case "tuic":
		input.Username, input.Secret, input.SupportUDP = p.Credential.Username, p.Credential.Password, true
	default:
		return input, importUnsupportedProtocol
	}
	// Hysteria2 and TUIC ride on QUIC but carry both TCP and UDP flows.
	if p.Protocol == "hysteria2" || p.Protocol == "tuic" {
		if q := p.QUIC; q != nil {
			if q.Obfs != "" {
				return input, importUnsupportedQUIC
			}
			if q.UpMbps != 0 || q.DownMbps != 0 || q.CongestionControl != "" || q.UDPRelayMode != "" {
				settings.QUIC = &desiredstate.QUICSettings{UpMbps: q.UpMbps, DownMbps: q.DownMbps, CongestionControl: q.CongestionControl, UDPRelayMode: q.UDPRelayMode}
			}
		}
	}
	if t := p.TLS; t != nil {
		if t.Insecure {
			return input, importInsecureTLS
		}
		if t.Fingerprint != "" {
			return input, importCertificatePin
		}
		settings.ServerName, settings.TrustedCAPEM, settings.ClientFingerprint = t.ServerName, t.PinnedCA, t.ClientFingerprint
		if p.Protocol == "vless" || p.Protocol == "trojan" {
			// Only h2 and http/1.1 run over a TCP stream; an h3 entry is a client
			// preference the server-side dialer cannot use.
			for _, alpn := range t.ALPN {
				if alpn == "h2" || alpn == "http/1.1" {
					settings.ALPN = append(settings.ALPN, alpn)
				}
			}
		}
	}
	if p.Security == "reality" && p.Reality != nil {
		reality := &desiredstate.RealityClientSettings{PublicKey: p.Reality.PublicKey}
		if p.Reality.ShortID != nil {
			reality.ShortID = *p.Reality.ShortID
		}
		if p.Reality.SpiderX != nil {
			reality.SpiderX = *p.Reality.SpiderX
		}
		settings.RealityClient = reality
		// Reality needs a ClientHello to imitate; clients default to Chrome too.
		if settings.ClientFingerprint == "" {
			settings.ClientFingerprint = "chrome"
		}
	}
	if t := p.Transport; t != nil && t.Type != "" && t.Type != "tcp" {
		switch t.Type {
		case "ws":
			settings.Path, settings.WSHost = t.Path, t.Host
		case "grpc":
			settings.ServiceName = t.ServiceName
		case "xhttp":
			if t.XHTTP == nil {
				return input, importUnsupportedTransport
			}
			settings.XHTTP = &desiredstate.XHTTPSettings{Host: t.XHTTP.Host, Path: t.XHTTP.Path, Mode: t.XHTTP.Mode}
		default:
			return input, importUnsupportedTransport
		}
		settings.Transport = t.Type
	}
	if settings.Security == "none" {
		settings.Security = ""
	}
	input.Settings = settings
	if ValidateOutbound(OutboundInput{Name: "import", NodeID: "node", Protocol: input.Protocol, Address: input.Address, Port: input.Port, FailurePolicy: input.FailurePolicy, SupportUDP: input.SupportUDP, Settings: input.Settings}) != nil {
		return input, importInvalidSettings
	}
	return input, ""
}

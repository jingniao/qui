package desiredstate

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Self-hosted xhttp and Reality settings (compatibility plan §13, phase M1).
//
// These are the server-private half of a VLESS listener and live only in the
// inbound's encrypted settings and the encrypted revision. The client-facing
// half (public key, Short ID, SNI, fingerprint) belongs to the shared
// subscription/proxy model (S3/S4) and is deliberately not defined here.
const (
	// SecurityReality authenticates against a real TLS site instead of
	// presenting a certificate.
	SecurityReality = "reality"
	// TransportXHTTP is Xray's HTTP-carried transport, bounded to the TCP/H2
	// modes below; nothing here claims HTTP/3 or QUIC.
	TransportXHTTP = "xhttp"
)

// The xhttp modes the pinned Xray carries over TCP (S0 evidence §4).
const (
	XHTTPModeAuto      = "auto"
	XHTTPModePacketUp  = "packet-up"
	XHTTPModeStreamUp  = "stream-up"
	XHTTPModeStreamOne = "stream-one"
)

// Implementation limits, not Owner choices.
const (
	MaxXHTTPHostLength        = 253
	MaxXHTTPPathLength        = 256
	MaxRealityServerNames     = 16
	MaxRealityServerNameLen   = 253
	MaxRealityShortIDs        = 16
	MaxRealityShortIDHexChars = 16
	RealityKeyBytes           = 32
)

// XHTTPSettings is the typed xhttp transport. Host stays separate from the TLS
// server name, and Path lives here rather than in the WebSocket path field.
type XHTTPSettings struct {
	Host string `json:"host,omitempty"`
	Path string `json:"path,omitempty"`
	Mode string `json:"mode,omitempty"`
}

// RealityServerSettings is the server-private half of a VLESS Reality inbound:
// PublicKey is the client-facing counterpart, the private key never leaves the
// encrypted settings, the revision or the Agent's own configuration.
type RealityServerSettings struct {
	PrivateKey      string   `json:"private_key"`
	PublicKey       string   `json:"public_key"`
	HandshakeTarget string   `json:"handshake_target"`
	ServerNames     []string `json:"server_names,omitempty"`
	ShortIDs        []string `json:"short_ids,omitempty"`
}

// ErrServerSettingsRendererPending marks settings this build stores and validates
// but cannot render yet (Xray in M2, sing-box in M4).
var ErrServerSettingsRendererPending = errors.New("xhttp/Reality settings cannot be rendered by this Agent yet")

// ValidateServerSettingsRenderer refuses a state that carries the M1 settings at
// the Agent's render entry, so a structurally valid but unrenderable listener can
// never be applied with these fields silently ignored.
func ValidateServerSettingsRenderer(state State) error {
	for _, inbound := range state.Inbounds {
		if inbound.Settings.HasServerSettings() {
			return fmt.Errorf("inbound %s: %w", inbound.ID, ErrServerSettingsRendererPending)
		}
	}
	for _, outbound := range state.Outbounds {
		if outbound.Settings.HasServerSettings() {
			return fmt.Errorf("outbound %s: %w", outbound.ID, ErrServerSettingsRendererPending)
		}
	}
	return nil
}

// HasServerSettings reports whether one settings block carries the server
// portion at all, so no layer has to test the fields in its own order.
func (s ProtocolSettings) HasServerSettings() bool {
	return s.Reality != nil || s.XHTTP != nil
}

// validateXHTTP checks the typed xhttp transport: an optional literal host, a
// rooted path and a mode the pinned core implements over TCP.
func (x *XHTTPSettings) validate() error {
	if x == nil {
		return errors.New("xhttp transport requires its host, path and mode settings")
	}
	if x.Host != "" {
		if len(x.Host) > MaxXHTTPHostLength || strings.ContainsAny(x.Host, ":/ \t\r\n") || !validForwardHost(x.Host) {
			return errors.New("invalid xhttp host")
		}
	}
	if len(x.Path) > MaxXHTTPPathLength {
		return errors.New("xhttp path exceeds limit")
	}
	if !strings.HasPrefix(x.Path, "/") {
		return errors.New("xhttp path must begin with slash")
	}
	if x.Mode != XHTTPModeAuto && x.Mode != XHTTPModePacketUp && x.Mode != XHTTPModeStreamUp && x.Mode != XHTTPModeStreamOne {
		return errors.New("unsupported xhttp mode")
	}
	return nil
}

// validateReality checks the server-private Reality settings: a role-correct
// X25519 key pair in unpadded base64url, an explicit host:port handshake target,
// bounded server names and Short IDs. Reachability is the Agent's native
// validation, not the panel's (AGENTS.md §13.1).
func (r *RealityServerSettings) validate() error {
	if r == nil {
		return errors.New("reality security requires its server settings")
	}
	privateKey, err := decodeRealityKey(r.PrivateKey)
	if err != nil {
		return errors.New("invalid reality private key")
	}
	publicKey, err := decodeRealityKey(r.PublicKey)
	if err != nil {
		return errors.New("invalid reality public key")
	}
	derived, err := ecdh.X25519().NewPrivateKey(privateKey)
	if err != nil {
		return errors.New("invalid reality private key")
	}
	if !bytes.Equal(derived.PublicKey().Bytes(), publicKey) {
		return errors.New("reality public key does not match the private key")
	}
	if r.HandshakeTarget == "" {
		return errors.New("reality requires the handshake target as host:port")
	}
	host, portText, err := net.SplitHostPort(r.HandshakeTarget)
	if err != nil || !validForwardHost(host) {
		return errors.New("invalid reality handshake target")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("invalid reality handshake target")
	}
	if len(r.ServerNames) == 0 || len(r.ServerNames) > MaxRealityServerNames {
		return errors.New("reality requires between one and sixteen server names")
	}
	for _, name := range r.ServerNames {
		if !validServerName(name) {
			return errors.New("invalid reality server name")
		}
	}
	if len(r.ShortIDs) == 0 || len(r.ShortIDs) > MaxRealityShortIDs {
		return errors.New("reality requires between one and sixteen short ids")
	}
	for _, id := range r.ShortIDs {
		if !validShortID(id) {
			return errors.New("invalid reality short id")
		}
	}
	return nil
}

// decodeRealityKey decodes a 32-byte X25519 key written in base64url without
// padding, the encoding Xray's own key output uses.
func decodeRealityKey(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("missing reality key")
	}
	key, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(key) != RealityKeyBytes {
		return nil, errors.New("invalid reality key")
	}
	return key, nil
}

// validServerName accepts a concrete DNS name. TLS clients do not send literal
// IP addresses as SNI, and every saved name must be usable in a subscription.
// The pinned Reality
// implementation matches ClientHello SNI by exact map lookup, not a wildcard.
func validServerName(name string) bool {
	if name == "" || len(name) > MaxRealityServerNameLen {
		return false
	}
	if net.ParseIP(name) != nil {
		return false
	}
	for _, char := range name {
		if char < 0x21 || char == 0x7f {
			return false
		}
	}
	return validForwardHost(name)
}

// validShortID accepts an even number of hex digits up to sixteen characters:
// the Short ID is a byte prefix of the handshake authentication.
func validShortID(value string) bool {
	if value == "" || len(value) > MaxRealityShortIDHexChars || len(value)%2 != 0 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}

// validateServerSettings validates the M1 portion of one protocol's settings.
// A settings block without the new fields returns immediately, so every legacy
// combination keeps its existing behaviour.
func validateServerSettings(protocol string, settings ProtocolSettings, inbound bool) error {
	// A security or transport that names the new half without its settings is
	// incomplete, whatever the other field is.
	if settings.Security == SecurityReality && settings.Reality == nil {
		return errors.New("reality security requires its server settings")
	}
	if settings.Transport == TransportXHTTP && settings.XHTTP == nil {
		return errors.New("xhttp transport requires its host, path and mode settings")
	}
	if !settings.HasServerSettings() {
		return nil
	}
	// M1 defines the server-private half only. An outbound carries client
	// parameters, which the shared subscription/proxy model adds once (S3/S4).
	if protocol != "vless" || !inbound {
		return errors.New("xhttp and reality settings require a VLESS inbound")
	}
	if settings.Method != "" || settings.WireGuard != nil || settings.QUIC != nil {
		return errors.New("xhttp and reality settings cannot carry other protocol options")
	}
	if settings.Reality != nil {
		// Reality authenticates against a real site instead of presenting a
		// certificate, so ordinary TLS material on the same listener is a
		// contradiction rather than an alternative.
		if settings.Security != SecurityReality {
			return errors.New("reality settings require reality security")
		}
		if settings.CertificatePEM != "" || settings.PrivateKeyPEM != "" || settings.TrustedCAPEM != "" {
			return errors.New("reality cannot carry TLS certificate material")
		}
		if err := settings.Reality.validate(); err != nil {
			return err
		}
	}
	if settings.XHTTP != nil {
		if settings.Transport != TransportXHTTP {
			return errors.New("xhttp settings require the xhttp transport")
		}
		// The path and the gRPC service name belong to their own transports; a
		// stale value would otherwise be rendered by one of them.
		if settings.Path != "" || settings.ServiceName != "" {
			return errors.New("xhttp keeps its own path and has no gRPC service name")
		}
		if err := settings.XHTTP.validate(); err != nil {
			return err
		}
	}
	return nil
}

// validateVision keeps the VLESS flow on the transports the pinned core accepts:
// XTLS Vision only on TCP with TLS or Reality. xhttp and gRPC flows are refused
// here instead of relying on native validation (S0 evidence §4).
func validateVision(protocol string, settings ProtocolSettings) error {
	if settings.Flow == "" {
		return nil
	}
	if protocol != "vless" || settings.Flow != "xtls-rprx-vision" ||
		settings.Security != "tls" && settings.Security != SecurityReality ||
		settings.Transport != "" && settings.Transport != "tcp" {
		return errors.New("VLESS flow requires TCP with TLS or Reality")
	}
	return nil
}

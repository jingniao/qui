// Package proxy holds the format-neutral client proxy model: the one shape a
// subscription source produces and a format exporter consumes. Formats and
// sources therefore never depend on each other, and neither depends on the
// panel's modules or on an HTTP server.
//
// The package imports nothing from proxycontrol: `user`, `desiredstate`,
// `system` and `net/http` must stay out of its dependency graph
// (docs/v1.1-plan.md 4.3, acceptance A1.5 of Phase 1).
package proxy

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"net/netip"
)

// ErrWireGuardKeyPair reports a client identity that no longer matches the
// published WireGuard peer. The text is user-facing and must stay unchanged:
// it tells the token holder to keep the old credential instead of resetting it.
var ErrWireGuardKeyPair = errors.New("WireGuard订阅密钥与已发布设备身份不一致；请保留原凭据，核对设备公钥及已发布配置")

// ErrWireGuardPeer reports a WireGuard endpoint without the peer material a
// client needs.
var ErrWireGuardPeer = errors.New("WireGuard peer configuration required")

// ErrWireGuardAddress reports an address a WireGuard client cannot use.
var ErrWireGuardAddress = errors.New("invalid WireGuard subscription address")

// Ref identifies something the source knows about: the entry node of a proxy.
type Ref struct {
	ID   string
	Name string
}

// Origin records where a proxy came from, so that rule templates and the admin
// UI can name it without knowing the source's own types.
//
// Ref is the source's own handle (for the published source it is the inbound
// ID). Labels are the attributes rule templates expand on; the published source
// emits "nodeGroup:<id>" for every granted node group that contains the node.
type Origin struct {
	Source string
	Ref    string
	Node   Ref
	Labels []string
}

// HasLabel reports whether the origin carries the given label.
func (o Origin) HasLabel(label string) bool {
	for _, value := range o.Labels {
		if value == label {
			return true
		}
	}
	return false
}

// Credential is the client side of a user credential. Password holds the
// private half for WireGuard, which is why it is never marshalled to JSON here;
// the admin preview builds its own DTO.
type Credential struct {
	Username  string
	Password  string
	PublicKey string
}

// TLS carries the certificate settings a client needs to accept the node.
type TLS struct {
	ServerName  string
	ALPN        []string
	PinnedCA    string
	Fingerprint string
	Insecure    bool
}

// Transport is the stream transport a client dials through.
type Transport struct {
	Type        string
	Path        string
	ServiceName string
}

// QUIC is the QUIC tuning a Hysteria2 or TUIC client can carry.
type QUIC struct {
	UpMbps            int
	DownMbps          int
	CongestionControl string
	UDPRelayMode      string
}

// WireGuard is the peer material of a WireGuard inbound.
type WireGuard struct {
	ServerPublicKey   string
	Addresses         []string
	MTU               int
	KeepAliveInterval int
}

// Proxy is one endpoint a client can dial.
//
// Security keeps the raw setting the panel published ("", "none" or "tls") so
// every exporter can reject a value it does not understand instead of guessing.
// TLS is set whenever the published settings carry certificate material (a "tls"
// security mode, a server name or a pinned CA); Security stays authoritative for
// whether TLS is negotiated. Flow and Method are protocol options (VLESS flow,
// Shadowsocks cipher).
type Proxy struct {
	// DialerProxy names the preceding hop of a client-only chain.
	DialerProxy string
	// ClientChain marks every cloned hop, including the first one, so formats
	// that cannot carry a chain omit the entire clone set.
	ClientChain string
	Origin      Origin
	Name        string
	Server      string
	Port        int
	Protocol    string
	Credential  Credential
	Flow        string
	Method      string
	Security    string
	TLS         *TLS
	Transport   *Transport
	QUIC        *QUIC
	WireGuard   *WireGuard
}

// Peer returns the WireGuard settings, or an error when the endpoint carries no
// peer material at all.
func (p Proxy) Peer() (*WireGuard, error) {
	if p.WireGuard == nil {
		return nil, ErrWireGuardPeer
	}
	return p.WireGuard, nil
}

// WireGuardAddresses validates the published identity and returns the client
// addresses a WireGuard client configures.
//
// The check order matches the pre-1.1 exporter: the private key and the public
// key derived from it must agree, the peer key must be a 32 byte X25519 key, and
// each address must be a single host address of a distinct family.
func (p Proxy) WireGuardAddresses() ([]string, error) {
	peer, err := p.Peer()
	if err != nil {
		return nil, err
	}
	privateKey, err := base64.StdEncoding.DecodeString(p.Credential.Password)
	defer clear(privateKey)
	if err != nil || len(privateKey) != 32 || base64.StdEncoding.EncodeToString(privateKey) != p.Credential.Password {
		return nil, ErrWireGuardKeyPair
	}
	key, err := ecdh.X25519().NewPrivateKey(privateKey)
	if err != nil || base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()) != p.Credential.PublicKey {
		return nil, ErrWireGuardKeyPair
	}
	serverPublic, err := base64.StdEncoding.DecodeString(peer.ServerPublicKey)
	if err != nil || len(serverPublic) != 32 || base64.StdEncoding.EncodeToString(serverPublic) != peer.ServerPublicKey {
		return nil, ErrWireGuardKeyPair
	}
	if peer.ServerPublicKey == "" || len(peer.Addresses) == 0 || len(peer.Addresses) > 2 {
		return nil, ErrWireGuardPeer
	}
	result := make([]string, 0, len(peer.Addresses))
	families := map[bool]bool{}
	for _, value := range peer.Addresses {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Is4In6() || prefix.Addr().IsUnspecified() || prefix.Addr().IsLoopback() || prefix.Addr().IsMulticast() || prefix.Bits() != prefix.Addr().BitLen() || families[prefix.Addr().Is4()] {
			return nil, ErrWireGuardAddress
		}
		families[prefix.Addr().Is4()] = true
		result = append(result, prefix.String())
	}
	return result, nil
}

// Warning is a non-fatal diagnostic produced while building or rendering a
// subscription. Warnings go to the admin preview and the logs, never to the
// token holder.
type Warning struct {
	// Stage is "source" or "format".
	Stage string
	// Ref is the source ID or the exporter ID that raised the warning.
	Ref     string
	Message string
}

// SourceWarning builds a warning raised by a subscription source.
func SourceWarning(id, message string) Warning {
	return Warning{Stage: "source", Ref: id, Message: message}
}

// FormatWarning builds a warning raised by a format exporter.
func FormatWarning(id, message string) Warning {
	return Warning{Stage: "format", Ref: id, Message: message}
}

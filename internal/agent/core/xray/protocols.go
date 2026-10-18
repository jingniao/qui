package xray

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/netip"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
)

type streamSettings struct {
	Network  string           `json:"network"`
	Security string           `json:"security"`
	TLS      *tlsSettings     `json:"tlsSettings,omitempty"`
	WS       *wsSettings      `json:"wsSettings,omitempty"`
	GRPC     *grpcSettings    `json:"grpcSettings,omitempty"`
	Sockopt  *sockoptSettings `json:"sockopt,omitempty"`
}
type sockoptSettings struct {
	Interface      string `json:"interface,omitempty"`
	DomainStrategy string `json:"domainStrategy,omitempty"`
}
type tlsSettings struct {
	ServerName        string           `json:"serverName,omitempty"`
	Certificates      []tlsCertificate `json:"certificates,omitempty"`
	DisableSystemRoot bool             `json:"disableSystemRoot,omitempty"`
	ALPN              []string         `json:"alpn,omitempty"`
}
type tlsCertificate struct {
	Certificate []string `json:"certificate"`
	Key         []string `json:"key,omitempty"`
	Usage       string   `json:"usage,omitempty"`
}
type wsSettings struct {
	Path string `json:"path"`
}
type grpcSettings struct {
	ServiceName string `json:"serviceName"`
}
type vlessAccount struct {
	ID         string `json:"id"`
	Encryption string `json:"encryption,omitempty"`
	Flow       string `json:"flow,omitempty"`
	Email      string `json:"email,omitempty"`
}
type vlessInbound struct {
	Clients    []vlessAccount `json:"clients"`
	Decryption string         `json:"decryption"`
}
type vlessTarget struct {
	Address string         `json:"address"`
	Port    int            `json:"port"`
	Users   []vlessAccount `json:"users"`
}
type vlessOutbound struct {
	Targets []vlessTarget `json:"vnext"`
}
type trojanAccount struct {
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
}
type trojanInbound struct {
	Clients []trojanAccount `json:"clients"`
}
type trojanTarget struct {
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
}
type trojanOutbound struct {
	Servers []trojanTarget `json:"servers"`
}
type proxyTarget struct {
	Address string    `json:"address"`
	Port    int       `json:"port"`
	Users   []account `json:"users,omitempty"`
}
type proxyOutbound struct {
	Servers []proxyTarget `json:"servers"`
}
type ssAccount struct {
	Method   string `json:"method"`
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
}
type ssInbound struct {
	Clients []ssAccount `json:"clients"`
	Network string      `json:"network"`
}
type ssTarget struct {
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Method   string `json:"method"`
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
}
type ssOutbound struct {
	Servers []ssTarget `json:"servers"`
}
type wireguardSettings struct {
	SecretKey   string          `json:"secretKey"`
	Address     []string        `json:"address"`
	Peers       []wireguardPeer `json:"peers"`
	MTU         int             `json:"mtu,omitempty"`
	NoKernelTun bool            `json:"noKernelTun"`
}
type wireguardPeer struct {
	PublicKey  string   `json:"publicKey"`
	Endpoint   string   `json:"endpoint,omitempty"`
	AllowedIPs []string `json:"allowedIPs"`
	KeepAlive  int      `json:"keepAlive,omitempty"`
	Email      string   `json:"email,omitempty"`
}

func stream(settings desiredstate.ProtocolSettings, inbound bool) (*streamSettings, error) {
	if settings.Transport == "" && settings.Security == "" {
		return nil, nil
	}
	if err := desiredstate.ValidateSettings("vless", settings, false); err != nil {
		return nil, err
	}
	network := settings.Transport
	if network == "" {
		network = "tcp"
	}
	security := settings.Security
	if security == "" {
		security = "none"
	}
	result := &streamSettings{Network: network, Security: security}
	if security == "tls" {
		result.TLS = &tlsSettings{ServerName: settings.ServerName}
		if inbound {
			result.TLS.Certificates = []tlsCertificate{{Certificate: strings.Split(strings.TrimSpace(settings.CertificatePEM), "\n"), Key: strings.Split(strings.TrimSpace(settings.PrivateKeyPEM), "\n")}}
		} else if settings.TrustedCAPEM != "" {
			result.TLS.Certificates = []tlsCertificate{{Certificate: strings.Split(strings.TrimSpace(settings.TrustedCAPEM), "\n"), Usage: "verify"}}
			result.TLS.DisableSystemRoot = true
		}
	}
	switch network {
	case "ws":
		result.WS = &wsSettings{Path: settings.Path}
		if result.WS.Path == "" {
			result.WS.Path = "/"
		}
	case "grpc":
		result.GRPC = &grpcSettings{ServiceName: settings.ServiceName}
	}
	return result, nil
}
func credentialEmail(credential desiredstate.Credential) string { return "credential:" + credential.ID }
func validUUID(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return false
	}
	data, err := hex.DecodeString(strings.Join(parts, ""))
	return err == nil && len(data) == 16
}
func method(settings desiredstate.ProtocolSettings) string {
	if settings.Method != "" {
		return settings.Method
	}
	return "chacha20-ietf-poly1305"
}
func buildInbound(inbound desiredstate.Inbound, credentials map[string]desiredstate.Credential) (string, any, *streamSettings, error) {
	transport, err := stream(inbound.Settings, true)
	if err != nil {
		return "", nil, nil, err
	}
	accounts := []desiredstate.Credential{}
	for _, id := range inbound.CredentialIDs {
		accounts = append(accounts, credentials[id])
	}
	switch inbound.Protocol {
	case "http", "socks5":
		settings := socksSettings{UDP: inbound.Protocol == "socks5"}
		protocol := "http"
		if inbound.Protocol == "socks5" {
			protocol = "socks"
			settings.Auth = "noauth"
		}
		if len(accounts) > 0 {
			if protocol == "socks" {
				settings.Auth = "password"
			}
			for _, credential := range accounts {
				if credential.Username == "" {
					return "", nil, nil, errors.New("HTTP/SOCKS5 credentials need a username")
				}
				settings.Accounts = append(settings.Accounts, account{User: credential.Username, Pass: credential.Secret})
			}
		}
		return protocol, settings, transport, nil
	case "vless":
		settings := vlessInbound{Decryption: "none", Clients: []vlessAccount{}}
		for _, credential := range accounts {
			if !validUUID(credential.Secret) {
				return "", nil, nil, errors.New("invalid VLESS UUID")
			}
			settings.Clients = append(settings.Clients, vlessAccount{ID: credential.Secret, Flow: inbound.Settings.Flow, Email: credentialEmail(credential)})
		}
		return "vless", settings, transport, nil
	case "trojan":
		settings := trojanInbound{Clients: []trojanAccount{}}
		for _, credential := range accounts {
			settings.Clients = append(settings.Clients, trojanAccount{Password: credential.Secret, Email: credentialEmail(credential)})
		}
		return "trojan", settings, transport, nil
	case "shadowsocks":
		settings := ssInbound{Clients: []ssAccount{}, Network: "tcp,udp"}
		for _, credential := range accounts {
			settings.Clients = append(settings.Clients, ssAccount{Method: method(inbound.Settings), Password: credential.Secret, Email: credentialEmail(credential)})
		}
		return "shadowsocks", settings, transport, nil
	case "wireguard":
		settings, err := buildWireGuard(inbound.Settings.WireGuard, credentials)
		return "wireguard", settings, nil, err
	}
	return "", nil, nil, errors.New("unsupported inbound protocol")
}
func buildOutbound(outbound desiredstate.Outbound, credentials map[string]desiredstate.Credential) (outboundConfig, error) {
	result := outboundConfig{Tag: outbound.ID}
	credential := credentials[outbound.CredentialID]
	transport, err := stream(outbound.Settings, false)
	if err != nil {
		return result, err
	}
	result.Stream = transport
	switch outbound.Protocol {
	case "direct":
		result.Protocol = "freedom"
	case "block":
		result.Protocol = "blackhole"
	case "http", "socks5":
		result.Protocol = outbound.Protocol
		if result.Protocol == "socks5" {
			result.Protocol = "socks"
		}
		target := proxyTarget{Address: outbound.Address, Port: outbound.Port}
		if outbound.CredentialID != "" {
			if credential.Username == "" {
				return result, errors.New("HTTP/SOCKS5 outbound requires username")
			}
			target.Users = []account{{User: credential.Username, Pass: credential.Secret}}
		}
		result.Settings = proxyOutbound{Servers: []proxyTarget{target}}
	case "vless":
		if !validUUID(credential.Secret) {
			return result, errors.New("VLESS outbound requires UUID credential")
		}
		result.Protocol = "vless"
		result.Settings = vlessOutbound{Targets: []vlessTarget{{Address: outbound.Address, Port: outbound.Port, Users: []vlessAccount{{ID: credential.Secret, Encryption: "none", Flow: outbound.Settings.Flow}}}}}
	case "trojan":
		if credential.Secret == "" {
			return result, errors.New("Trojan outbound requires credential")
		}
		result.Protocol = "trojan"
		result.Settings = trojanOutbound{Servers: []trojanTarget{{Address: outbound.Address, Port: outbound.Port, Password: credential.Secret}}}
	case "shadowsocks":
		if credential.Secret == "" {
			return result, errors.New("Shadowsocks outbound requires credential")
		}
		result.Protocol = "shadowsocks"
		result.Settings = ssOutbound{Servers: []ssTarget{{Address: outbound.Address, Port: outbound.Port, Method: method(outbound.Settings), Password: credential.Secret}}}
	case "wireguard":
		result.Protocol = "wireguard"
		result.Stream = nil
		result.Settings, err = buildWireGuard(outbound.Settings.WireGuard, credentials)
		if err != nil {
			return result, err
		}
	default:
		return result, errors.New("unsupported outbound protocol")
	}
	return result, nil
}
func buildWireGuard(settings *desiredstate.WireGuardSettings, credentials map[string]desiredstate.Credential) (wireguardSettings, error) {
	if settings == nil {
		return wireguardSettings{}, errors.New("WireGuard settings required")
	}
	addresses := []string{}
	for _, value := range settings.Addresses {
		var address netip.Addr
		if prefix, err := netip.ParsePrefix(value); err == nil {
			address = prefix.Addr()
		} else {
			var err error
			address, err = netip.ParseAddr(value)
			if err != nil {
				return wireguardSettings{}, errors.New("invalid WireGuard interface address")
			}
		}
		address = address.Unmap()
		bits := 128
		if address.Is4() {
			bits = 32
		}
		addresses = append(addresses, netip.PrefixFrom(address, bits).String())
	}
	result := wireguardSettings{SecretKey: settings.SecretKey, Address: addresses, MTU: settings.MTU, NoKernelTun: true, Peers: []wireguardPeer{}}
	for _, peer := range settings.Peers {
		publicKey := peer.PublicKey
		email := ""
		if peer.CredentialID != "" {
			credential, exists := credentials[peer.CredentialID]
			if !exists || credential.Protocol != "wireguard" {
				return result, errors.New("WireGuard peer credential missing")
			}
			if credential.PublicKey != "" {
				publicKey = credential.PublicKey
			} else {
				raw, err := base64.StdEncoding.DecodeString(credential.Secret)
				if err != nil {
					return result, errors.New("invalid WireGuard credential")
				}
				key, err := ecdh.X25519().NewPrivateKey(raw)
				if err != nil {
					return result, errors.New("invalid WireGuard credential")
				}
				publicKey = base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
			}
			email = credentialEmail(credential)
		}
		result.Peers = append(result.Peers, wireguardPeer{PublicKey: publicKey, Endpoint: peer.Endpoint, AllowedIPs: peer.AllowedIPs, KeepAlive: peer.KeepAlive, Email: email})
	}
	return result, nil
}

package singbox

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"net"
	"net/netip"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
)

type wireGuardEndpoint struct {
	Type             string          `json:"type"`
	Tag              string          `json:"tag"`
	System           bool            `json:"system"`
	Address          []string        `json:"address"`
	PrivateKey       string          `json:"private_key"`
	MTU              int             `json:"mtu,omitempty"`
	Peers            []wireGuardPeer `json:"peers"`
	ListenPort       int             `json:"listen_port,omitempty"`
	BindInterface    string          `json:"bind_interface,omitempty"`
	Inet4BindAddress string          `json:"inet4_bind_address,omitempty"`
	Inet6BindAddress string          `json:"inet6_bind_address,omitempty"`
	DomainResolver   *domainResolver `json:"domain_resolver,omitempty"`
}
type wireGuardPeer struct {
	Address    string   `json:"address"`
	Port       int      `json:"port"`
	PublicKey  string   `json:"public_key"`
	AllowedIPs []string `json:"allowed_ips"`
	Keepalive  int      `json:"persistent_keepalive_interval,omitempty"`
}

func hasWireGuardOutbound(capabilities *pb.Capabilities) bool {
	if capabilities == nil {
		return false
	}
	for _, p := range capabilities.Protocols {
		if p.Protocol == "wireguard" && p.Outbound {
			return true
		}
	}
	return false
}
func hasWireGuardInbound(capabilities *pb.Capabilities) bool {
	if capabilities == nil {
		return false
	}
	for _, protocol := range capabilities.Protocols {
		if protocol.Protocol == "wireguard" && protocol.Inbound {
			return true
		}
	}
	return false
}

func compileWireGuardInbound(entry desiredstate.Inbound, credentials map[string]desiredstate.Credential) (wireGuardEndpoint, error) {
	result := wireGuardEndpoint{Type: "wireguard", Tag: entry.ID, System: false, ListenPort: entry.Port, Address: []string{}, Peers: []wireGuardPeer{}}
	settings := entry.Settings.WireGuard
	if settings == nil || len(settings.Peers) == 0 || len(settings.Peers) > 256 {
		return result, errors.New("WireGuard inbound requires bounded authenticated peers")
	}
	listen, err := netip.ParseAddr(entry.Listen)
	if err != nil || listen.Is4In6() || listen.Zone() != "" || listen.IsMulticast() {
		return result, errors.New("invalid WireGuard inbound listener")
	}
	result.PrivateKey, result.MTU = settings.SecretKey, settings.MTU
	for _, value := range settings.Addresses {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			address, parseErr := netip.ParseAddr(value)
			if parseErr != nil {
				return result, parseErr
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		if prefix.Addr().Is4In6() {
			return result, errors.New("mapped WireGuard inbound address unsupported")
		}
		result.Address = append(result.Address, prefix.String())
	}
	allowed, seen := map[string]bool{}, map[string]bool{}
	addresses, publicKeys := map[netip.Addr]bool{}, map[string]bool{}
	for _, id := range entry.CredentialIDs {
		allowed[id] = true
	}
	for _, peer := range settings.Peers {
		credential, exists := credentials[peer.CredentialID]
		if !exists || !allowed[peer.CredentialID] || credential.Protocol != "wireguard" || seen[credential.ID] {
			return result, errors.New("WireGuard inbound peer credential unavailable or duplicated")
		}
		public := credential.PublicKey
		if public == "" {
			secret, err := base64.StdEncoding.DecodeString(credential.Secret)
			if err != nil {
				return result, errors.New("invalid WireGuard peer identity")
			}
			key, err := ecdh.X25519().NewPrivateKey(secret)
			clear(secret)
			if err != nil {
				return result, errors.New("invalid WireGuard peer identity")
			}
			public = base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
		}
		peerKey, err := base64.StdEncoding.DecodeString(peer.PublicKey)
		if err != nil || len(peerKey) != 32 || base64.StdEncoding.EncodeToString(peerKey) != public || publicKeys[public] {
			return result, errors.New("WireGuard inbound peer public key conflicts with credential")
		}
		if len(peer.AllowedIPs) == 0 || len(peer.AllowedIPs) > 2 {
			return result, errors.New("WireGuard inbound peer requires one host address per family")
		}
		compiled := wireGuardPeer{PublicKey: public, AllowedIPs: []string{}, Keepalive: peer.KeepAlive}
		families := map[bool]bool{}
		for _, value := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(value)
			if err != nil || prefix.Addr().Is4In6() || prefix.Bits() != prefix.Addr().BitLen() || addresses[prefix.Addr()] || families[prefix.Addr().Is4()] {
				return result, errors.New("WireGuard inbound peer host address is ambiguous")
			}
			addresses[prefix.Addr()], families[prefix.Addr().Is4()] = true, true
			compiled.AllowedIPs = append(compiled.AllowedIPs, prefix.String())
		}
		if peer.Endpoint != "" {
			host, portText, err := net.SplitHostPort(peer.Endpoint)
			port, portErr := strconv.Atoi(portText)
			if err != nil || portErr != nil || host == "" || port < 1 || port > 65535 {
				return result, errors.New("invalid WireGuard inbound peer endpoint")
			}
			compiled.Address, compiled.Port = host, port
		}
		result.Peers = append(result.Peers, compiled)
		seen[credential.ID], publicKeys[public] = true, true
	}
	for _, id := range entry.CredentialIDs {
		credential, exists := credentials[id]
		if !exists || !seen[id] && !credential.Internal {
			return result, errors.New("WireGuard device credential has no authenticated peer")
		}
	}
	return result, nil
}
func compileWireGuardEndpoint(entry desiredstate.Outbound, credentials map[string]desiredstate.Credential) (wireGuardEndpoint, error) {
	result := wireGuardEndpoint{Type: "wireguard", Tag: entry.ID, System: false, Address: []string{}, Peers: []wireGuardPeer{}}
	settings := entry.Settings.WireGuard
	if settings == nil {
		return result, errors.New("WireGuard endpoint settings required")
	}
	result.PrivateKey = settings.SecretKey
	result.MTU = settings.MTU
	for _, value := range settings.Addresses {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			address, err := netip.ParseAddr(value)
			if err != nil {
				return result, err
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		if prefix.Addr().Is4In6() {
			bits := prefix.Bits() - 96
			if bits < 0 {
				return result, errors.New("ambiguous mapped WireGuard interface prefix")
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), bits)
		}
		result.Address = append(result.Address, prefix.String())
	}
	for _, peer := range settings.Peers {
		host, portText, err := net.SplitHostPort(peer.Endpoint)
		if err != nil || host == "" {
			return result, errors.New("WireGuard outbound peer requires host:port endpoint")
		}
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return result, errors.New("invalid WireGuard endpoint port")
		}
		public := peer.PublicKey
		if peer.CredentialID != "" {
			credential, exists := credentials[peer.CredentialID]
			if !exists || credential.Protocol != "wireguard" {
				return result, errors.New("WireGuard peer credential unavailable")
			}
			public = credential.PublicKey
			if public == "" {
				raw, err := base64.StdEncoding.DecodeString(credential.Secret)
				if err != nil {
					return result, err
				}
				key, err := ecdh.X25519().NewPrivateKey(raw)
				clear(raw)
				if err != nil {
					return result, err
				}
				public = base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
			}
		}
		if len(peer.AllowedIPs) == 0 {
			return result, errors.New("WireGuard outbound peer requires allowed networks")
		}
		result.Peers = append(result.Peers, wireGuardPeer{Address: host, Port: port, PublicKey: public, AllowedIPs: peer.AllowedIPs, Keepalive: peer.KeepAlive})
	}
	if len(result.Peers) == 0 {
		return result, errors.New("WireGuard outbound requires at least one peer")
	}
	return result, nil
}

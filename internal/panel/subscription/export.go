package subscription

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

var ErrWireGuardKeyPair = errors.New("WireGuard订阅密钥与已发布设备身份不一致；请保留原凭据，核对设备公钥及已发布配置")

func uri(endpoint Endpoint) (string, error) {
	name := endpoint.NodeName + " · " + endpoint.Protocol
	target := net.JoinHostPort(endpoint.Address, strconv.Itoa(endpoint.Port))
	user := url.UserPassword(endpoint.Credential.Username, endpoint.Credential.Secret)
	switch endpoint.Protocol {
	case "http", "socks5":
		return (&url.URL{Scheme: endpoint.Protocol, Host: target, User: user, Fragment: name}).String(), nil
	case "trojan", "vless":
		u := url.URL{Scheme: endpoint.Protocol, Host: target, User: url.User(endpoint.Credential.Secret), Fragment: name}
		query := url.Values{}
		security := endpoint.Settings.Security
		if security == "" {
			security = "none"
		}
		query.Set("security", security)
		transport := endpoint.Settings.Transport
		if transport == "" {
			transport = "tcp"
		}
		query.Set("type", transport)
		if transport == "ws" {
			query.Set("path", endpoint.Settings.Path)
		}
		if transport == "grpc" {
			query.Set("serviceName", endpoint.Settings.ServiceName)
		}
		if endpoint.Settings.ServerName != "" {
			query.Set("sni", endpoint.Settings.ServerName)
		}
		if endpoint.Protocol == "vless" {
			query.Set("encryption", "none")
			if endpoint.Settings.Flow != "" {
				query.Set("flow", endpoint.Settings.Flow)
			}
		}
		if security != "none" && security != "tls" {
			return "", errors.New("unsupported subscription TLS settings")
		}
		u.RawQuery = query.Encode()
		return u.String(), nil
	case "shadowsocks":
		method := endpoint.Settings.Method
		if method == "" {
			method = "chacha20-ietf-poly1305"
		}
		userinfo := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + endpoint.Credential.Secret))
		return "ss://" + userinfo + "@" + target + "#" + url.PathEscape(name), nil
	case "hysteria2", "tuic":
		if endpoint.Settings.TrustedCAPEM != "" {
			return "", errors.New("custom QUIC CA material requires sing-box subscription format")
		}
		u := url.URL{Scheme: endpoint.Protocol, Host: target, Fragment: name, User: url.User(endpoint.Credential.Secret)}
		if endpoint.Protocol == "tuic" {
			u.User = user
		}
		query := url.Values{}
		query.Set("sni", endpoint.Settings.ServerName)
		query.Set("alpn", "h3")
		if q := endpoint.Settings.QUIC; q != nil {
			if endpoint.Protocol == "hysteria2" {
				if q.UpMbps > 0 {
					query.Set("upmbps", strconv.Itoa(q.UpMbps))
				}
				if q.DownMbps > 0 {
					query.Set("downmbps", strconv.Itoa(q.DownMbps))
				}
			} else {
				if q.CongestionControl != "" {
					query.Set("congestion_control", q.CongestionControl)
				}
				if q.UDPRelayMode != "" {
					query.Set("udp_relay_mode", q.UDPRelayMode)
				}
			}
		}
		u.RawQuery = query.Encode()
		return u.String(), nil
	case "wireguard":
		addresses, err := wireguardEndpointAddresses(endpoint)
		if err != nil {
			return "", err
		}
		query := url.Values{}
		query.Set("publickey", endpoint.WireGuardPublicKey)
		query.Set("address", strings.Join(addresses, ","))
		if endpoint.Settings.WireGuard != nil && endpoint.Settings.WireGuard.MTU > 0 {
			query.Set("mtu", strconv.Itoa(endpoint.Settings.WireGuard.MTU))
		}
		return (&url.URL{Scheme: "wireguard", Host: target, User: url.User(endpoint.Credential.Secret), RawQuery: query.Encode(), Fragment: name}).String(), nil
	default:
		return "", errors.New("unsupported subscription protocol")
	}
}

type singOutbound struct {
	Type              string         `json:"type"`
	Tag               string         `json:"tag"`
	Server            string         `json:"server"`
	ServerPort        int            `json:"server_port"`
	Username          string         `json:"username,omitempty"`
	Password          string         `json:"password,omitempty"`
	UUID              string         `json:"uuid,omitempty"`
	Method            string         `json:"method,omitempty"`
	Flow              string         `json:"flow,omitempty"`
	Version           string         `json:"version,omitempty"`
	TLS               *singTLS       `json:"tls,omitempty"`
	Transport         *singTransport `json:"transport,omitempty"`
	UpMbps            int            `json:"up_mbps,omitempty"`
	DownMbps          int            `json:"down_mbps,omitempty"`
	CongestionControl string         `json:"congestion_control,omitempty"`
	UDPRelayMode      string         `json:"udp_relay_mode,omitempty"`
}
type singTransport struct {
	Type        string `json:"type"`
	Path        string `json:"path,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
}
type singTLS struct {
	Enabled     bool     `json:"enabled"`
	ServerName  string   `json:"server_name,omitempty"`
	Certificate []string `json:"certificate,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
}
type singWG struct {
	Type       string       `json:"type"`
	Tag        string       `json:"tag"`
	Address    []string     `json:"address"`
	PrivateKey string       `json:"private_key"`
	MTU        int          `json:"mtu,omitempty"`
	Peers      []singWGPeer `json:"peers"`
}
type singWGPeer struct {
	Address    string   `json:"address"`
	Port       int      `json:"port"`
	PublicKey  string   `json:"public_key"`
	AllowedIPs []string `json:"allowed_ips"`
	KeepAlive  int      `json:"persistent_keepalive_interval"`
}

func Export(endpoints []Endpoint, format string) ([]byte, error) {
	if format == "" {
		format = "uri"
	}
	if format == "sing-box" {
		outbounds := []singOutbound{}
		wireguards := []singWG{}
		for _, endpoint := range endpoints {
			if endpoint.Protocol == "wireguard" {
				addresses, err := wireguardEndpointAddresses(endpoint)
				if err != nil {
					return nil, err
				}
				endpointConfig := singWG{Type: "wireguard", Tag: endpoint.NodeName + "-" + endpoint.InboundID, Address: addresses, PrivateKey: endpoint.Credential.Secret, Peers: []singWGPeer{{Address: endpoint.Address, Port: endpoint.Port, PublicKey: endpoint.WireGuardPublicKey, AllowedIPs: []string{"0.0.0.0/0", "::/0"}, KeepAlive: 15}}}
				if endpoint.Settings.WireGuard != nil {
					endpointConfig.MTU = endpoint.Settings.WireGuard.MTU
				}
				wireguards = append(wireguards, endpointConfig)
				continue
			}
			outbound := singOutbound{Type: endpoint.Protocol, Tag: fmt.Sprintf("%s-%s-%s", endpoint.NodeName, endpoint.InboundID, endpoint.Protocol), Server: endpoint.Address, ServerPort: endpoint.Port}
			switch endpoint.Protocol {
			case "http":
				outbound.Username = endpoint.Credential.Username
				outbound.Password = endpoint.Credential.Secret
			case "socks5":
				outbound.Type = "socks"
				outbound.Version = "5"
				outbound.Username = endpoint.Credential.Username
				outbound.Password = endpoint.Credential.Secret
			case "trojan", "hysteria2":
				outbound.Password = endpoint.Credential.Secret
			case "tuic":
				outbound.UUID = endpoint.Credential.Username
				outbound.Password = endpoint.Credential.Secret
			case "vless":
				outbound.UUID = endpoint.Credential.Secret
				outbound.Flow = endpoint.Settings.Flow
			case "shadowsocks":
				outbound.Method = endpoint.Settings.Method
				if outbound.Method == "" {
					outbound.Method = "chacha20-ietf-poly1305"
				}
				outbound.Password = endpoint.Credential.Secret
			default:
				return nil, errors.New("protocol requires additional sing-box peer configuration")
			}
			if q := endpoint.Settings.QUIC; q != nil {
				outbound.UpMbps = q.UpMbps
				outbound.DownMbps = q.DownMbps
				outbound.CongestionControl = q.CongestionControl
				outbound.UDPRelayMode = q.UDPRelayMode
			}
			if endpoint.Settings.Security == "tls" {
				outbound.TLS = &singTLS{Enabled: true, ServerName: endpoint.Settings.ServerName}
				if endpoint.Protocol == "hysteria2" || endpoint.Protocol == "tuic" {
					outbound.TLS.ALPN = []string{"h3"}
				}
				if endpoint.Settings.TrustedCAPEM != "" {
					outbound.TLS.Certificate = strings.Split(strings.TrimSpace(endpoint.Settings.TrustedCAPEM), "\n")
				}
			} else if endpoint.Settings.Security != "" && endpoint.Settings.Security != "none" {
				return nil, errors.New("unsupported subscription TLS settings")
			}
			if transport := endpoint.Settings.Transport; transport != "" && transport != "tcp" && transport != "quic" {
				if transport != "ws" && transport != "grpc" {
					return nil, errors.New("subscription transport is not configured")
				}
				outbound.Transport = &singTransport{Type: transport, Path: endpoint.Settings.Path, ServiceName: endpoint.Settings.ServiceName}
			}
			outbounds = append(outbounds, outbound)
		}
		return json.MarshalIndent(struct {
			Outbounds []singOutbound `json:"outbounds"`
			Endpoints []singWG       `json:"endpoints,omitempty"`
		}{Outbounds: outbounds, Endpoints: wireguards}, "", "  ")
	}
	if format != "uri" && format != "base64" {
		return nil, errors.New("format must be uri, base64 or sing-box")
	}
	uris := []string{}
	for _, endpoint := range endpoints {
		value, err := uri(endpoint)
		if err != nil {
			return nil, err
		}
		uris = append(uris, value)
	}
	data := []byte(strings.Join(uris, "\n"))
	if len(data) > 0 {
		data = append(data, '\n')
	}
	if format == "base64" {
		data = []byte(base64.StdEncoding.EncodeToString(data))
	}
	return data, nil
}

func wireguardEndpointAddresses(endpoint Endpoint) ([]string, error) {
	private, err := base64.StdEncoding.DecodeString(endpoint.Credential.Secret)
	defer clear(private)
	if err != nil || len(private) != 32 || base64.StdEncoding.EncodeToString(private) != endpoint.Credential.Secret {
		return nil, ErrWireGuardKeyPair
	}
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil || base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()) != endpoint.Credential.PublicKey {
		return nil, ErrWireGuardKeyPair
	}
	public, err := base64.StdEncoding.DecodeString(endpoint.WireGuardPublicKey)
	if err != nil || len(public) != 32 || base64.StdEncoding.EncodeToString(public) != endpoint.WireGuardPublicKey {
		return nil, ErrWireGuardKeyPair
	}
	addresses := endpoint.WireGuardAddresses
	if len(addresses) == 0 && endpoint.WireGuardAddress != "" {
		addresses = []string{endpoint.WireGuardAddress}
	}
	if len(addresses) == 0 || len(addresses) > 2 || endpoint.WireGuardPublicKey == "" {
		return nil, errors.New("WireGuard peer configuration required")
	}
	if endpoint.WireGuardAddress != "" && endpoint.WireGuardAddress != addresses[0] {
		return nil, errors.New("WireGuard primary address mismatch")
	}
	result := make([]string, 0, len(addresses))
	families := map[bool]bool{}
	for _, value := range addresses {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Is4In6() || prefix.Addr().IsUnspecified() || prefix.Addr().IsLoopback() || prefix.Addr().IsMulticast() || prefix.Bits() != prefix.Addr().BitLen() || families[prefix.Addr().Is4()] {
			return nil, errors.New("invalid WireGuard subscription address")
		}
		families[prefix.Addr().Is4()] = true
		result = append(result, prefix.String())
	}
	return result, nil
}

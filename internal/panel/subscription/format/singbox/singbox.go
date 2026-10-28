// Package singbox renders the sing-box fragment subscription: the outbounds and
// WireGuard endpoints of the user's published servers, with no inbound, route or
// DNS section.
//
// The document is byte-identical to the pre-1.1 output (acceptance A1.1). The
// official sing-box apps cannot import the fragment as a profile; a complete
// profile format would be a separate exporter (`sing-box-profile`), not built
// yet.
package singbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/proxy"
)

// keepAlive is the persistent keepalive the fragment always told clients to use.
const keepAlive = 15

// Exporter is the "sing-box" format.
type Exporter struct{}

func (Exporter) ID() string          { return "sing-box" }
func (Exporter) Aliases() []string   { return nil }
func (Exporter) ContentType() string { return "application/json; charset=utf-8" }
func (Exporter) FileName() string    { return "qui-sing-box.json" }

// UserAgents returns the client patterns for this format. v1.1 ships none: no
// client User-Agent could be verified from an official source
// (docs/v1.1-phase0-findings.md A.7).
func (Exporter) UserAgents() []string { return nil }

func (Exporter) UsesRules() bool { return false }

type outbound struct {
	Type              string     `json:"type"`
	Tag               string     `json:"tag"`
	Server            string     `json:"server"`
	ServerPort        int        `json:"server_port"`
	Username          string     `json:"username,omitempty"`
	Password          string     `json:"password,omitempty"`
	UUID              string     `json:"uuid,omitempty"`
	Method            string     `json:"method,omitempty"`
	Flow              string     `json:"flow,omitempty"`
	Version           string     `json:"version,omitempty"`
	TLS               *tls       `json:"tls,omitempty"`
	Transport         *transport `json:"transport,omitempty"`
	UpMbps            int        `json:"up_mbps,omitempty"`
	DownMbps          int        `json:"down_mbps,omitempty"`
	CongestionControl string     `json:"congestion_control,omitempty"`
	UDPRelayMode      string     `json:"udp_relay_mode,omitempty"`
}

type transport struct {
	Type        string `json:"type"`
	Path        string `json:"path,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
}

type tls struct {
	Enabled     bool     `json:"enabled"`
	ServerName  string   `json:"server_name,omitempty"`
	Certificate []string `json:"certificate,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
}

type wireguardEndpoint struct {
	Type       string          `json:"type"`
	Tag        string          `json:"tag"`
	Address    []string        `json:"address"`
	PrivateKey string          `json:"private_key"`
	MTU        int             `json:"mtu,omitempty"`
	Peers      []wireguardPeer `json:"peers"`
}

type wireguardPeer struct {
	Address    string   `json:"address"`
	Port       int      `json:"port"`
	PublicKey  string   `json:"public_key"`
	AllowedIPs []string `json:"allowed_ips"`
	KeepAlive  int      `json:"persistent_keepalive_interval"`
}

func (Exporter) Render(profile format.Profile) (format.Output, error) {
	output := format.Output{}
	outbounds := []outbound{}
	endpoints := []wireguardEndpoint{}
	for _, endpoint := range profile.Proxies {
		if endpoint.DialerProxy != "" || endpoint.ClientChain != "" {
			output.Warnings = append(output.Warnings, proxy.FormatWarning("sing-box", "Client chain omitted: format does not support client chains"))
			continue
		}
		if endpoint.Protocol == "wireguard" {
			peer, err := endpoint.Peer()
			if err != nil {
				return format.Output{}, err
			}
			addresses, err := endpoint.WireGuardAddresses()
			if err != nil {
				return format.Output{}, err
			}
			endpoints = append(endpoints, wireguardEndpoint{Type: "wireguard", Tag: endpoint.Origin.Node.Name + "-" + endpoint.Origin.Ref, Address: addresses, PrivateKey: endpoint.Credential.Password, MTU: peer.MTU, Peers: []wireguardPeer{{Address: endpoint.Server, Port: endpoint.Port, PublicKey: peer.ServerPublicKey, AllowedIPs: []string{"0.0.0.0/0", "::/0"}, KeepAlive: keepAlive}}})
			continue
		}
		value, err := renderOutbound(endpoint)
		if err != nil {
			return format.Output{}, err
		}
		outbounds = append(outbounds, value)
	}
	body, err := json.MarshalIndent(struct {
		Outbounds []outbound          `json:"outbounds"`
		Endpoints []wireguardEndpoint `json:"endpoints,omitempty"`
	}{Outbounds: outbounds, Endpoints: endpoints}, "", "  ")
	if err != nil {
		return format.Output{}, err
	}
	output.Body = body
	return output, nil
}

func renderOutbound(endpoint proxy.Proxy) (outbound, error) {
	value := outbound{Type: endpoint.Protocol, Tag: fmt.Sprintf("%s-%s-%s", endpoint.Origin.Node.Name, endpoint.Origin.Ref, endpoint.Protocol), Server: endpoint.Server, ServerPort: endpoint.Port}
	switch endpoint.Protocol {
	case "http":
		value.Username = endpoint.Credential.Username
		value.Password = endpoint.Credential.Password
	case "socks5":
		value.Type = "socks"
		value.Version = "5"
		value.Username = endpoint.Credential.Username
		value.Password = endpoint.Credential.Password
	case "trojan", "hysteria2":
		value.Password = endpoint.Credential.Password
	case "tuic":
		value.UUID = endpoint.Credential.Username
		value.Password = endpoint.Credential.Password
	case "vless":
		value.UUID = endpoint.Credential.Password
		value.Flow = endpoint.Flow
	case "shadowsocks":
		value.Method = endpoint.Method
		if value.Method == "" {
			value.Method = "chacha20-ietf-poly1305"
		}
		value.Password = endpoint.Credential.Password
	default:
		return outbound{}, errors.New("protocol requires additional sing-box peer configuration")
	}
	if q := endpoint.QUIC; q != nil {
		value.UpMbps = q.UpMbps
		value.DownMbps = q.DownMbps
		value.CongestionControl = q.CongestionControl
		value.UDPRelayMode = q.UDPRelayMode
	}
	if endpoint.Security == "tls" {
		value.TLS = &tls{Enabled: true, ServerName: serverName(endpoint)}
		if endpoint.Protocol == "hysteria2" || endpoint.Protocol == "tuic" {
			value.TLS.ALPN = []string{"h3"}
		}
		if pinned := pinnedCA(endpoint); pinned != "" {
			value.TLS.Certificate = strings.Split(strings.TrimSpace(pinned), "\n")
		}
	} else if endpoint.Security != "" && endpoint.Security != "none" {
		return outbound{}, errors.New("unsupported subscription TLS settings")
	}
	if endpoint.Transport != nil {
		if kind := endpoint.Transport.Type; kind != "" && kind != "tcp" && kind != "quic" {
			if kind != "ws" && kind != "grpc" {
				return outbound{}, errors.New("subscription transport is not configured")
			}
			value.Transport = &transport{Type: kind, Path: endpoint.Transport.Path, ServiceName: endpoint.Transport.ServiceName}
		}
	}
	return value, nil
}

func serverName(endpoint proxy.Proxy) string {
	if endpoint.TLS == nil {
		return ""
	}
	return endpoint.TLS.ServerName
}

func pinnedCA(endpoint proxy.Proxy) string {
	if endpoint.TLS == nil {
		return ""
	}
	return endpoint.TLS.PinnedCA
}

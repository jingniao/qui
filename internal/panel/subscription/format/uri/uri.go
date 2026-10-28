// Package uri renders a subscription as a list of share URIs and as that same
// list in Base64. The bytes are the ones the pre-1.1 exporter produced: the
// formats are pinned by the fixtures in testdata (acceptance A1.1).
package uri

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/proxy"
)

// ErrUnrepresentable marks a proxy a URI cannot describe at all, so the list
// formats leave it out instead of failing the whole document.
var ErrUnrepresentable = errors.New("endpoint has no URI representation")

// URI is the "uri" exporter: one share URI per line.
type URI struct{}

func (URI) ID() string          { return "uri" }
func (URI) Aliases() []string   { return nil }
func (URI) ContentType() string { return "text/plain; charset=utf-8" }
func (URI) FileName() string    { return "qui-uri.txt" }

// UserAgents returns the client patterns for this format. v1.1 ships none: no
// client User-Agent could be verified from an official source
// (docs/v1.1-phase0-findings.md A.7).
func (URI) UserAgents() []string { return nil }

func (URI) UsesRules() bool { return false }

func (URI) Render(profile format.Profile) (format.Output, error) {
	return renderList("uri", profile.Proxies)
}

// Base64 is the "base64" exporter: the URI list, Base64 encoded.
type Base64 struct{}

func (Base64) ID() string          { return "base64" }
func (Base64) Aliases() []string   { return nil }
func (Base64) ContentType() string { return "text/plain; charset=utf-8" }
func (Base64) FileName() string    { return "qui-base64.txt" }

// UserAgents returns the client patterns for this format; see URI.UserAgents.
func (Base64) UserAgents() []string { return nil }

func (Base64) UsesRules() bool { return false }

func (Base64) Render(profile format.Profile) (format.Output, error) {
	output, err := renderList("base64", profile.Proxies)
	if err != nil {
		return format.Output{}, err
	}
	output.Body = []byte(base64.StdEncoding.EncodeToString(output.Body))
	return output, nil
}

func renderList(exporterID string, proxies []proxy.Proxy) (format.Output, error) {
	output := format.Output{}
	lines := []string{}
	for _, endpoint := range proxies {
		if endpoint.DialerProxy != "" || endpoint.ClientChain != "" {
			output.Warnings = append(output.Warnings, proxy.FormatWarning(exporterID, "Client chain omitted: format does not support dialer-proxy"))
			continue
		}
		value, err := Link(endpoint)
		if errors.Is(err, ErrUnrepresentable) {
			// A URI list has nowhere to put the CA a Hysteria2 or TUIC endpoint
			// pins, and one such endpoint used to fail the whole document: the
			// token holder then lost every other link as well. Skipping it keeps
			// the rest, and the sing-box format still lists it with its
			// certificate.
			output.Warnings = append(output.Warnings, proxy.FormatWarning(exporterID, endpoint.Protocol+" endpoint with a pinned CA is left out of the URI list"))
			continue
		}
		if err != nil {
			return format.Output{}, err
		}
		lines = append(lines, value)
	}
	data := []byte(strings.Join(lines, "\n"))
	if len(data) > 0 {
		data = append(data, '\n')
	}
	output.Body = data
	return output, nil
}

// Link renders one proxy as a share URI.
func Link(endpoint proxy.Proxy) (string, error) {
	if endpoint.DialerProxy != "" || endpoint.ClientChain != "" {
		return "", ErrUnrepresentable
	}
	name := endpoint.Name
	target := net.JoinHostPort(endpoint.Server, strconv.Itoa(endpoint.Port))
	user := url.UserPassword(endpoint.Credential.Username, endpoint.Credential.Password)
	switch endpoint.Protocol {
	case "http", "socks5":
		return (&url.URL{Scheme: endpoint.Protocol, Host: target, User: user, Fragment: name}).String(), nil
	case "trojan", "vless":
		u := url.URL{Scheme: endpoint.Protocol, Host: target, User: url.User(endpoint.Credential.Password), Fragment: name}
		query := url.Values{}
		security := endpoint.Security
		if security == "" {
			security = "none"
		}
		query.Set("security", security)
		transport := "tcp"
		if endpoint.Transport != nil && endpoint.Transport.Type != "" {
			transport = endpoint.Transport.Type
		}
		query.Set("type", transport)
		if transport == "ws" && endpoint.Transport != nil {
			query.Set("path", endpoint.Transport.Path)
		}
		if transport == "grpc" && endpoint.Transport != nil {
			query.Set("serviceName", endpoint.Transport.ServiceName)
		}
		if serverName := tlsServerName(endpoint); serverName != "" {
			query.Set("sni", serverName)
		}
		if endpoint.Protocol == "vless" {
			query.Set("encryption", "none")
			if endpoint.Flow != "" {
				query.Set("flow", endpoint.Flow)
			}
		}
		if security != "none" && security != "tls" {
			return "", errors.New("unsupported subscription TLS settings")
		}
		u.RawQuery = query.Encode()
		return u.String(), nil
	case "shadowsocks":
		method := endpoint.Method
		if method == "" {
			method = "chacha20-ietf-poly1305"
		}
		userinfo := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + endpoint.Credential.Password))
		return "ss://" + userinfo + "@" + target + "#" + url.PathEscape(name), nil
	case "hysteria2", "tuic":
		if pinnedCA(endpoint) != "" {
			// A hysteria2 or tuic URI has no field for a pinned CA, so the
			// endpoint only survives in the sing-box format, which carries the
			// certificate itself.
			return "", ErrUnrepresentable
		}
		u := url.URL{Scheme: endpoint.Protocol, Host: target, Fragment: name, User: url.User(endpoint.Credential.Password)}
		if endpoint.Protocol == "tuic" {
			u.User = user
		}
		query := url.Values{}
		query.Set("sni", tlsServerName(endpoint))
		query.Set("alpn", "h3")
		if q := endpoint.QUIC; q != nil {
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
		addresses, err := endpoint.WireGuardAddresses()
		if err != nil {
			return "", err
		}
		query := url.Values{}
		query.Set("publickey", endpoint.WireGuard.ServerPublicKey)
		query.Set("address", strings.Join(addresses, ","))
		if endpoint.WireGuard.MTU > 0 {
			query.Set("mtu", strconv.Itoa(endpoint.WireGuard.MTU))
		}
		return (&url.URL{Scheme: "wireguard", Host: target, User: url.User(endpoint.Credential.Password), RawQuery: query.Encode(), Fragment: name}).String(), nil
	default:
		return "", errors.New("unsupported subscription protocol")
	}
}

func tlsServerName(endpoint proxy.Proxy) string {
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

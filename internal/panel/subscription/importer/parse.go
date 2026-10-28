package importer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
)

var ErrParse = errors.New("subscription has no supported valid proxies")

type URI struct{}
type Base64 struct{}
type Clash struct{}

func (URI) Detect(b []byte) bool { return bytes.Contains(b, []byte("://")) }
func (Base64) Detect(b []byte) bool {
	decoded, err := decode64(string(bytes.TrimSpace(b)))
	return err == nil && (URI{}).Detect(decoded)
}
func (Clash) Detect(b []byte) bool { return bytes.Contains(b, []byte("proxies:")) }
func decode64(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, ErrParse
}
func Parsers() []format.Parser { return []format.Parser{Clash{}, URI{}, Base64{}} }

// Parse reports a generic diagnostic without echoing untrusted input, which may
// include a password, a token or the source's private subscription URL.
func Parse(b []byte) ([]proxy.Proxy, []proxy.Warning, error) {
	if len(b) > MaxBody {
		return nil, nil, ErrParse
	}
	for _, parser := range Parsers() {
		if parser.Detect(b) {
			p, err := parser.Parse(b)
			return p, []proxy.Warning{proxy.SourceWarning("imported", "Only supported valid proxy entries are imported; unsupported fields and entries are omitted.")}, err
		}
	}
	return nil, nil, ErrParse
}
func valid(p proxy.Proxy) bool {
	// YAML escapes and aliases can expand during typed decoding. Bound the
	// canonical cache representation too, so every accepted import is backupable.
	encoded, err := json.Marshal(p)
	if err != nil || len(encoded) > MaxBody {
		return false
	}
	if len(p.Credential.Password) > 4096 || p.TLS != nil && p.TLS.Insecure {
		return false
	}
	if p.Protocol == "vless" && desiredstate.ValidateUUID(p.Credential.Password) != nil {
		return false
	}
	if p.Protocol == "tuic" && desiredstate.ValidateUUID(p.Credential.Username) != nil {
		return false
	}
	if p.Protocol == "shadowsocks" {
		switch p.Method {
		case "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305":
		default:
			return false
		}
	}
	if p.Name == "" || len(p.Name) > 200 || p.Server == "" || len(p.Server) > 253 || p.Port < 1 || p.Port > 65535 {
		return false
	}
	if strings.ContainsAny(p.Server, " /\\\r\n\t@") || strings.IndexFunc(p.Name, unicode.IsControl) >= 0 {
		return false
	}
	switch p.Protocol {
	case "socks5", "http":
		return true
	case "vless", "trojan", "shadowsocks", "hysteria2", "tuic":
		return p.Credential.Password != ""
	}
	return false
}
func (URI) Parse(b []byte) ([]proxy.Proxy, error) {
	if len(b) > MaxBody {
		return nil, ErrParse
	}
	result := []proxy.Proxy{}
	for _, line := range strings.Split(string(b), "\n") {
		u, err := url.Parse(strings.TrimSpace(line))
		if err != nil || u.Hostname() == "" {
			continue
		}
		p := proxy.Proxy{Name: u.Fragment, Server: u.Hostname(), Protocol: u.Scheme}
		p.Port, _ = strconv.Atoi(u.Port())
		if p.Name == "" {
			p.Name = net.JoinHostPort(p.Server, u.Port())
		}
		if u.User != nil {
			p.Credential.Username = u.User.Username()
			p.Credential.Password, _ = u.User.Password()
		}
		q := u.Query()
		switch p.Protocol {
		case "vless", "trojan", "hysteria2", "tuic":
			if u.User == nil {
				continue
			}
			p.Credential.Password = u.User.Username()
			if p.Protocol == "tuic" {
				p.Credential.Username = u.User.Username()
				p.Credential.Password, _ = u.User.Password()
			}
		case "ss":
			p.Protocol = "shadowsocks"
			p.Method = p.Credential.Username
			if p.Credential.Password == "" {
				decoded, e := decode64(p.Method)
				if e != nil {
					continue
				}
				p.Method, p.Credential.Password, _ = strings.Cut(string(decoded), ":")
			}
		case "socks":
			p.Protocol = "socks5"
		}
		if len(q) > 0 {
			known := true
			for key := range q {
				switch key {
				case "security", "sni", "peer", "alpn", "type", "path", "serviceName", "flow":
				default:
					known = false
				}
			}
			if !known {
				continue
			}
		}
		p.Flow = q.Get("flow")
		p.Security = q.Get("security")
		if p.Protocol == "trojan" || p.Protocol == "hysteria2" || p.Protocol == "tuic" {
			p.Security = "tls"
		}
		if p.Security != "" && p.Security != "none" && p.Security != "tls" {
			continue
		}
		if p.Security == "tls" {
			sni := q.Get("sni")
			if sni == "" {
				sni = q.Get("peer")
			}
			p.TLS = &proxy.TLS{ServerName: sni}
			if q.Get("alpn") != "" {
				p.TLS.ALPN = strings.Split(q.Get("alpn"), ",")
			}
		}
		if transport := q.Get("type"); transport != "" && transport != "tcp" {
			if transport != "ws" && transport != "grpc" {
				continue
			}
			p.Transport = &proxy.Transport{Type: transport, Path: q.Get("path"), ServiceName: q.Get("serviceName")}
		}
		if valid(p) {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil, ErrParse
	}
	return result, nil
}
func (Base64) Parse(b []byte) ([]proxy.Proxy, error) {
	if len(b) > MaxBody {
		return nil, ErrParse
	}
	d, e := decode64(string(bytes.TrimSpace(b)))
	if e != nil {
		return nil, ErrParse
	}
	return (URI{}).Parse(d)
}

type clashEntry struct {
	Name       string   `yaml:"name"`
	Type       string   `yaml:"type"`
	Server     string   `yaml:"server"`
	Port       int      `yaml:"port"`
	Username   string   `yaml:"username"`
	Password   string   `yaml:"password"`
	UUID       string   `yaml:"uuid"`
	Cipher     string   `yaml:"cipher"`
	TLS        bool     `yaml:"tls"`
	SNI        string   `yaml:"sni"`
	ServerName string   `yaml:"servername"`
	ALPN       []string `yaml:"alpn"`
	Insecure   bool     `yaml:"skip-cert-verify"`
	UDP        bool     `yaml:"udp"`
	Network    string   `yaml:"network"`
	Flow       string   `yaml:"flow"`
	WS         *struct {
		Path string `yaml:"path"`
	} `yaml:"ws-opts"`
	GRPC *struct {
		Service string `yaml:"grpc-service-name"`
	} `yaml:"grpc-opts"`
}

func (Clash) Parse(b []byte) ([]proxy.Proxy, error) {
	if len(b) > MaxBody {
		return nil, ErrParse
	}
	var document struct {
		Proxies []yaml.Node `yaml:"proxies"`
	}
	if yaml.Unmarshal(b, &document) != nil {
		return nil, ErrParse
	}
	result := []proxy.Proxy{}
	for _, node := range document.Proxies {
		raw, err := yaml.Marshal(&node)
		if err != nil {
			continue
		}
		var entry clashEntry
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		decoder.KnownFields(true)
		if decoder.Decode(&entry) != nil {
			continue
		}
		p := proxy.Proxy{Name: entry.Name, Protocol: entry.Type, Server: entry.Server, Port: entry.Port, Method: entry.Cipher, Flow: entry.Flow, Credential: proxy.Credential{Username: entry.Username, Password: entry.Password}}
		if p.Protocol == "ss" {
			p.Protocol = "shadowsocks"
		}
		if p.Protocol == "vless" {
			p.Credential.Password = entry.UUID
		}
		if p.Protocol == "tuic" {
			p.Credential.Username = entry.UUID
		}
		if entry.TLS || p.Protocol == "trojan" || p.Protocol == "hysteria2" || p.Protocol == "tuic" {
			p.Security = "tls"
			sni := entry.SNI
			if sni == "" {
				sni = entry.ServerName
			}
			p.TLS = &proxy.TLS{ServerName: sni, ALPN: entry.ALPN, Insecure: entry.Insecure}
		}
		switch entry.Network {
		case "", "tcp":
		case "ws":
			if entry.WS == nil {
				continue
			}
			p.Transport = &proxy.Transport{Type: "ws", Path: entry.WS.Path}
		case "grpc":
			if entry.GRPC == nil {
				continue
			}
			p.Transport = &proxy.Transport{Type: "grpc", ServiceName: entry.GRPC.Service}
		default:
			continue
		}
		if valid(p) {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil, ErrParse
	}
	return result, nil
}

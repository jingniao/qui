// Package clash renders a full Clash profile: proxies, the rule template's
// groups, rule providers and rules. Two capability sets share one renderer:
//
//   - clash (aliases mihomo, clash-meta): every protocol qui publishes, in
//     mihomo's field names (docs/v1.1-phase0-findings.md item 1);
//   - clash-legacy (alias clash-premium): only what the archived open-source and
//     Premium cores are documented to accept (item 2e): ss, socks5, http and
//     trojan, and no GEOSITE, RULE-SET or rule providers.
//
// Whatever a capability set cannot express is left out with a warning; a rule's
// target is never rewritten. The document is marshalled through typed structs
// and yaml.v3, so names are quoted by the YAML library, never by hand.
package clash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

// capabilities is what one Clash dialect accepts.
type capabilities struct {
	id        string
	protocols map[string]bool
	rules     map[string]bool
}

var meta = capabilities{
	id:        "clash",
	protocols: map[string]bool{"http": true, "socks5": true, "shadowsocks": true, "vless": true, "trojan": true, "hysteria2": true, "tuic": true, "wireguard": true},
	rules: map[string]bool{rules.RuleDomain: true, rules.RuleDomainSuffix: true, rules.RuleDomainKeyword: true, rules.RuleIPCIDR: true, rules.RuleIPCIDR6: true, rules.RuleGeoIP: true,
		rules.RuleGeoSite: true, rules.RuleDstPort: true, rules.RuleSet: true, rules.RuleMatch: true},
}

var legacy = capabilities{
	id:        "clash-legacy",
	protocols: map[string]bool{"http": true, "socks5": true, "shadowsocks": true, "trojan": true},
	rules: map[string]bool{rules.RuleDomain: true, rules.RuleDomainSuffix: true, rules.RuleDomainKeyword: true, rules.RuleIPCIDR: true, rules.RuleIPCIDR6: true, rules.RuleGeoIP: true,
		rules.RuleDstPort: true, rules.RuleMatch: true},
}

// Meta is the "clash" exporter, the profile every Clash client not identified
// as legacy gets.
type Meta struct{}

func (Meta) ID() string           { return meta.id }
func (Meta) Aliases() []string    { return []string{"mihomo", "clash-meta"} }
func (Meta) ContentType() string  { return "text/yaml; charset=utf-8" }
func (Meta) FileName() string     { return "qui-clash.yaml" }
func (Meta) UserAgents() []string { return nil }
func (Meta) UsesRules() bool      { return true }
func (Meta) Render(profile format.Profile) (format.Output, error) {
	return render(meta, profile)
}

// Legacy is the "clash-legacy" exporter for the archived Clash and Clash
// Premium cores.
type Legacy struct{}

func (Legacy) ID() string           { return legacy.id }
func (Legacy) Aliases() []string    { return []string{"clash-premium"} }
func (Legacy) ContentType() string  { return "text/yaml; charset=utf-8" }
func (Legacy) FileName() string     { return "qui-clash-legacy.yaml" }
func (Legacy) UserAgents() []string { return nil }
func (Legacy) UsesRules() bool      { return true }
func (Legacy) Render(profile format.Profile) (format.Output, error) {
	return render(legacy, profile)
}

type document struct {
	MixedPort     int                     `yaml:"mixed-port"`
	AllowLAN      bool                    `yaml:"allow-lan"`
	Mode          string                  `yaml:"mode"`
	LogLevel      string                  `yaml:"log-level"`
	IPv6          bool                    `yaml:"ipv6"`
	DNS           *dnsSection             `yaml:"dns,omitempty"`
	Proxies       []clashProxy            `yaml:"proxies"`
	ProxyGroups   []group                 `yaml:"proxy-groups"`
	RuleProviders map[string]ruleProvider `yaml:"rule-providers,omitempty"`
	Rules         []string                `yaml:"rules"`
}

type dnsSection struct {
	Enable       bool     `yaml:"enable"`
	IPv6         bool     `yaml:"ipv6"`
	EnhancedMode string   `yaml:"enhanced-mode,omitempty"`
	Nameserver   []string `yaml:"nameserver,omitempty"`
	Fallback     []string `yaml:"fallback,omitempty"`
}

type group struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Proxies  []string `yaml:"proxies"`
	URL      string   `yaml:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty"`
}

type ruleProvider struct {
	Type     string `yaml:"type"`
	Behavior string `yaml:"behavior"`
	Format   string `yaml:"format,omitempty"`
	URL      string `yaml:"url"`
	Path     string `yaml:"path"`
	Interval int    `yaml:"interval"`
}

type wsOptions struct {
	Path string `yaml:"path,omitempty"`
}

type grpcOptions struct {
	ServiceName string `yaml:"grpc-service-name,omitempty"`
}

// clashProxy is one proxy in mihomo's field names; zero fields are omitted.
type clashProxy struct {
	DialerProxy          string       `yaml:"dialer-proxy,omitempty"`
	Name                 string       `yaml:"name"`
	Type                 string       `yaml:"type"`
	Server               string       `yaml:"server"`
	Port                 int          `yaml:"port"`
	Username             string       `yaml:"username,omitempty"`
	Password             string       `yaml:"password,omitempty"`
	UUID                 string       `yaml:"uuid,omitempty"`
	Cipher               string       `yaml:"cipher,omitempty"`
	Flow                 string       `yaml:"flow,omitempty"`
	UDP                  bool         `yaml:"udp,omitempty"`
	TLS                  bool         `yaml:"tls,omitempty"`
	ServerName           string       `yaml:"servername,omitempty"`
	SNI                  string       `yaml:"sni,omitempty"`
	ALPN                 []string     `yaml:"alpn,omitempty"`
	Fingerprint          string       `yaml:"fingerprint,omitempty"`
	Network              string       `yaml:"network,omitempty"`
	WSOptions            *wsOptions   `yaml:"ws-opts,omitempty"`
	GRPCOptions          *grpcOptions `yaml:"grpc-opts,omitempty"`
	Up                   string       `yaml:"up,omitempty"`
	Down                 string       `yaml:"down,omitempty"`
	CongestionController string       `yaml:"congestion-controller,omitempty"`
	UDPRelayMode         string       `yaml:"udp-relay-mode,omitempty"`
	PrivateKey           string       `yaml:"private-key,omitempty"`
	PublicKey            string       `yaml:"public-key,omitempty"`
	IP                   string       `yaml:"ip,omitempty"`
	IPv6                 string       `yaml:"ipv6,omitempty"`
	AllowedIPs           []string     `yaml:"allowed-ips,omitempty"`
	MTU                  int          `yaml:"mtu,omitempty"`
	PersistentKeepalive  int          `yaml:"persistent-keepalive,omitempty"`
}

// reserved are names Clash gives a meaning of its own; a proxy may not take one.
var reserved = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true}

func render(set capabilities, profile format.Profile) (format.Output, error) {
	output := format.Output{}
	warn := func(message string) {
		output.Warnings = append(output.Warnings, proxy.FormatWarning(set.id, message))
	}
	resolved := profile.Rules
	if resolved == nil {
		value := rules.Expand(rules.Default(), profile.Proxies)
		resolved = &value
	}
	groupNames := map[string]bool{}
	for _, group := range resolved.Groups {
		groupNames[group.Name] = true
	}
	// Proxies: drop what this dialect cannot carry, then give every proxy a name
	// that collides with no group, no reserved word and no other proxy. renamed
	// maps the expanded template's proxy names onto the names in the document.
	renamed := map[string]string{}
	used := map[string]bool{}
	doc := document{Mode: resolved.Client.Mode, MixedPort: resolved.Client.MixedPort, AllowLAN: resolved.Client.AllowLAN, LogLevel: resolved.Client.LogLevel, IPv6: resolved.Client.IPv6, Proxies: []clashProxy{}, ProxyGroups: []group{}, Rules: []string{}}
	for _, endpoint := range profile.Proxies {
		if (endpoint.DialerProxy != "" || endpoint.ClientChain != "") && set.id != "clash" {
			warn("Client chain omitted: format does not support dialer-proxy")
			continue
		}
		if !set.protocols[endpoint.Protocol] {
			warn(endpoint.Name + "：" + endpoint.Protocol + " 不在该格式支持的协议中，已省略")
			continue
		}
		value, err := convert(endpoint)
		if err != nil {
			warn(endpoint.Name + "：" + err.Error() + "，已省略")
			continue
		}
		name := endpoint.Name
		for suffix := 2; used[name] || groupNames[name] || reserved[strings.ToUpper(name)]; suffix++ {
			name = endpoint.Name + " (" + strconv.Itoa(suffix) + ")"
		}
		used[name] = true
		renamed[endpoint.Name] = name
		value.Name = name
		doc.Proxies = append(doc.Proxies, value)
	}
	// Resolve dependencies after capability filtering and collision renaming.
	for pass := 0; pass <= len(profile.Proxies); pass++ {
		changed := false
		for _, endpoint := range profile.Proxies {
			name, exists := renamed[endpoint.Name]
			if !exists || endpoint.DialerProxy == "" {
				continue
			}
			dialer, ok := renamed[endpoint.DialerProxy]
			if !ok {
				delete(renamed, endpoint.Name)
				for i, p := range doc.Proxies {
					if p.Name == name {
						doc.Proxies = append(doc.Proxies[:i], doc.Proxies[i+1:]...)
						break
					}
				}
				warn("Client chain omitted: preceding hop unavailable")
				changed = true
				continue
			}
			for i := range doc.Proxies {
				if doc.Proxies[i].Name == name {
					doc.Proxies[i].DialerProxy = dialer
				}
			}
		}
		if !changed {
			break
		}
	}
	if resolved.DNS.Enable {
		doc.DNS = &dnsSection{Enable: true, IPv6: resolved.Client.IPv6, EnhancedMode: resolved.DNS.EnhancedMode, Nameserver: resolved.DNS.Nameservers, Fallback: resolved.DNS.Fallback}
	}
	for _, source := range resolved.Groups {
		members := []string{}
		for _, member := range source.Members {
			if member.Kind != rules.KindProxy {
				members = append(members, member.Name)
			} else if name, ok := renamed[member.Name]; ok {
				members = append(members, name)
			}
			// A proxy this dialect left out is simply not a member here.
		}
		if len(members) == 0 {
			members = []string{rules.TargetDirect}
			warn("策略组 " + source.Name + " 在该格式下没有可用节点，已改为 DIRECT")
		}
		value := group{Name: source.Name, Type: source.Type, Proxies: members}
		if source.Type != "select" {
			value.URL, value.Interval = source.TestURL, source.IntervalSeconds
		}
		doc.ProxyGroups = append(doc.ProxyGroups, value)
	}
	providers := map[string]rules.ResolvedProvider{}
	for _, provider := range resolved.Providers {
		providers[provider.Name] = provider
	}
	for _, rule := range resolved.Rules {
		if !set.rules[rule.Type] {
			warn(rule.Type + " 规则不在该格式支持的规则中，已省略")
			continue
		}
		target := rule.Target.Name
		if rule.Target.Kind == rules.KindProxy {
			mapped, ok := renamed[target]
			if !ok {
				warn("规则目标 " + target + " 在该格式下不可用，该规则已省略")
				continue
			}
			target = mapped
		}
		if rule.Type == rules.RuleSet {
			provider, ok := providers[rule.Payload]
			if !ok {
				warn("规则集 " + rule.Payload + " 未定义，该规则已省略")
				continue
			}
			value := ruleProvider{Type: "http", Behavior: provider.Behavior, Format: provider.Format, URL: provider.URL, Path: "./ruleset/" + providerFile(provider.Name), Interval: provider.IntervalSeconds}
			if provider.ServiceSetID != "" {
				if profile.ProviderURL != nil {
					value.URL = profile.ProviderURL(provider.ServiceSetID)
				} else {
					// The Owner's preview has no subscription token; the real
					// profile carries the user's own link here.
					value.URL = "https://<panel>/sub/<token>/rules/" + provider.ServiceSetID
				}
			}
			if doc.RuleProviders == nil {
				doc.RuleProviders = map[string]ruleProvider{}
			}
			doc.RuleProviders[provider.Name] = value
		}
		line := rule.Type + "," + rule.Payload + "," + target
		if rule.Type == rules.RuleMatch {
			line = rules.RuleMatch + "," + target
		}
		if rule.NoResolve {
			line += ",no-resolve"
		}
		doc.Rules = append(doc.Rules, line)
	}
	if len(doc.Rules) == 0 || !strings.HasPrefix(doc.Rules[len(doc.Rules)-1], rules.RuleMatch+",") {
		// The template always ends in MATCH; a dialect that dropped it (never the
		// case today) must still end in a final rule.
		doc.Rules = append(doc.Rules, rules.RuleMatch+","+rules.TargetDirect)
		warn("规则末尾缺少 MATCH，已补充 MATCH,DIRECT")
	}
	body, err := yaml.Marshal(doc)
	if err != nil {
		return format.Output{}, err
	}
	output.Body = body
	return output, nil
}

// providerFile is the cache file name of a rule provider on the client, reduced
// to a safe alphabet and made unique by a short hash of the provider name.
func providerFile(name string) string {
	var builder strings.Builder
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
			builder.WriteRune(char)
		} else {
			builder.WriteRune('_')
		}
	}
	sum := sha256.Sum256([]byte(name))
	return builder.String() + "-" + hex.EncodeToString(sum[:4]) + ".rules"
}

// certificateFingerprint is the SHA-256 of the first certificate of a PEM
// bundle: mihomo's fingerprint pin matches any certificate of the chain the
// server presents, so pinning the published CA works (findings item 1b).
func certificateFingerprint(bundle string) (string, error) {
	block, _ := pem.Decode([]byte(bundle))
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("固定的 CA 证书无法解析")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

func serverName(endpoint proxy.Proxy) string {
	if endpoint.TLS == nil {
		return ""
	}
	return endpoint.TLS.ServerName
}

func convert(endpoint proxy.Proxy) (clashProxy, error) {
	value := clashProxy{Server: endpoint.Server, Port: endpoint.Port}
	tls := false
	switch endpoint.Security {
	case "", "none":
	case "tls":
		tls = true
	default:
		return value, fmt.Errorf("不支持的 TLS 设置 %q", endpoint.Security)
	}
	transport := ""
	if endpoint.Transport != nil {
		transport = endpoint.Transport.Type
	}
	stream := func() error {
		switch transport {
		case "", "tcp":
		case "ws":
			value.Network = "ws"
			value.WSOptions = &wsOptions{Path: endpoint.Transport.Path}
		case "grpc":
			value.Network = "grpc"
			value.GRPCOptions = &grpcOptions{ServiceName: endpoint.Transport.ServiceName}
		default:
			return fmt.Errorf("不支持的传输方式 %q", transport)
		}
		return nil
	}
	pin := func() error {
		if endpoint.TLS == nil || endpoint.TLS.PinnedCA == "" {
			return nil
		}
		fingerprint, err := certificateFingerprint(endpoint.TLS.PinnedCA)
		if err != nil {
			return err
		}
		value.Fingerprint = fingerprint
		return nil
	}
	switch endpoint.Protocol {
	case "http":
		value.Type, value.Username, value.Password, value.TLS = "http", endpoint.Credential.Username, endpoint.Credential.Password, tls
		if tls {
			value.SNI = serverName(endpoint)
		}
	case "socks5":
		value.Type, value.Username, value.Password, value.TLS, value.UDP = "socks5", endpoint.Credential.Username, endpoint.Credential.Password, tls, true
	case "shadowsocks":
		value.Type, value.Password, value.UDP = "ss", endpoint.Credential.Password, true
		value.Cipher = endpoint.Method
		if value.Cipher == "" {
			value.Cipher = "chacha20-ietf-poly1305"
		}
	case "vless":
		value.Type, value.UUID, value.Flow, value.UDP, value.TLS = "vless", endpoint.Credential.Password, endpoint.Flow, true, tls
		if tls {
			value.ServerName = serverName(endpoint)
		}
		if err := stream(); err != nil {
			return value, err
		}
	case "trojan":
		value.Type, value.Password, value.UDP = "trojan", endpoint.Credential.Password, true
		value.SNI = serverName(endpoint)
		if err := stream(); err != nil {
			return value, err
		}
	case "hysteria2":
		value.Type, value.Password, value.SNI, value.ALPN = "hysteria2", endpoint.Credential.Password, serverName(endpoint), []string{"h3"}
		if q := endpoint.QUIC; q != nil {
			if q.UpMbps > 0 {
				value.Up = strconv.Itoa(q.UpMbps) + " Mbps"
			}
			if q.DownMbps > 0 {
				value.Down = strconv.Itoa(q.DownMbps) + " Mbps"
			}
		}
		if err := pin(); err != nil {
			return value, err
		}
	case "tuic":
		value.Type, value.UUID, value.Password, value.SNI, value.ALPN, value.UDP = "tuic", endpoint.Credential.Username, endpoint.Credential.Password, serverName(endpoint), []string{"h3"}, true
		if q := endpoint.QUIC; q != nil {
			value.CongestionController, value.UDPRelayMode = q.CongestionControl, q.UDPRelayMode
		}
		if err := pin(); err != nil {
			return value, err
		}
	case "wireguard":
		addresses, err := endpoint.WireGuardAddresses()
		if err != nil {
			return value, err
		}
		value.Type, value.PrivateKey, value.PublicKey, value.UDP = "wireguard", endpoint.Credential.Password, endpoint.WireGuard.ServerPublicKey, true
		value.AllowedIPs, value.MTU, value.PersistentKeepalive = []string{"0.0.0.0/0", "::/0"}, endpoint.WireGuard.MTU, 15
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address)
			if err != nil {
				return value, err
			}
			if prefix.Addr().Is4() {
				value.IP = prefix.Addr().String()
			} else {
				value.IPv6 = prefix.Addr().String()
			}
		}
	default:
		return value, fmt.Errorf("不支持的协议 %q", endpoint.Protocol)
	}
	return value, nil
}

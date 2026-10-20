// Package singbox adapts typed DesiredState to a single local sing-box process.
package singbox

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/scheduling"
	"proxycontrol/pkg/contracts/desiredstate"
	policy "proxycontrol/pkg/contracts/policy"
	"proxycontrol/pkg/contracts/scheduler"
	"strconv"
	"strings"
	"time"
)

type document struct {
	Endpoints    []wireGuardEndpoint  `json:"endpoints,omitempty"`
	Log          logOptions           `json:"log"`
	Inbounds     []inbound            `json:"inbounds"`
	Outbounds    []outbound           `json:"outbounds"`
	Route        routeOptions         `json:"route"`
	DNS          dnsOptions           `json:"dns"`
	Experimental *experimentalOptions `json:"experimental,omitempty"`
}

type dnsOptions struct {
	Servers []dnsServer `json:"servers"`
	Final   string      `json:"final"`
}
type dnsServer struct {
	Type       string              `json:"type"`
	Tag        string              `json:"tag"`
	Predefined map[string][]string `json:"predefined,omitempty"`
}
type logOptions struct {
	Disabled  bool   `json:"disabled"`
	Level     string `json:"level,omitempty"`
	Output    string `json:"output,omitempty"`
	Timestamp bool   `json:"timestamp,omitempty"`
}
type tlsOptions struct {
	Enabled     bool     `json:"enabled"`
	ServerName  string   `json:"server_name,omitempty"`
	Certificate []string `json:"certificate,omitempty"`
	Key         []string `json:"key,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
}
type transportOptions struct {
	Type        string `json:"type"`
	Path        string `json:"path,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
}
type userOptions struct {
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	Flow     string `json:"flow,omitempty"`
}
type inbound struct {
	Type              string            `json:"type"`
	Tag               string            `json:"tag"`
	Listen            string            `json:"listen"`
	Port              int               `json:"listen_port"`
	Users             []userOptions     `json:"users,omitempty"`
	Method            string            `json:"method,omitempty"`
	Password          string            `json:"password,omitempty"`
	TLS               *tlsOptions       `json:"tls,omitempty"`
	Transport         *transportOptions `json:"transport,omitempty"`
	UpMbps            int               `json:"up_mbps,omitempty"`
	DownMbps          int               `json:"down_mbps,omitempty"`
	CongestionControl string            `json:"congestion_control,omitempty"`
}
type outbound struct {
	Type              string            `json:"type"`
	Tag               string            `json:"tag"`
	Outbounds         []string          `json:"outbounds,omitempty"`
	URL               string            `json:"url,omitempty"`
	Interval          string            `json:"interval,omitempty"`
	Server            string            `json:"server,omitempty"`
	Port              int               `json:"server_port,omitempty"`
	Username          string            `json:"username,omitempty"`
	Password          string            `json:"password,omitempty"`
	UUID              string            `json:"uuid,omitempty"`
	Flow              string            `json:"flow,omitempty"`
	Method            string            `json:"method,omitempty"`
	TLS               *tlsOptions       `json:"tls,omitempty"`
	Transport         *transportOptions `json:"transport,omitempty"`
	UpMbps            int               `json:"up_mbps,omitempty"`
	DownMbps          int               `json:"down_mbps,omitempty"`
	CongestionControl string            `json:"congestion_control,omitempty"`
	UDPRelayMode      string            `json:"udp_relay_mode,omitempty"`
	BindInterface     string            `json:"bind_interface,omitempty"`
	Inet4BindAddress  string            `json:"inet4_bind_address,omitempty"`
	Inet6BindAddress  string            `json:"inet6_bind_address,omitempty"`
	DomainResolver    *domainResolver   `json:"domain_resolver,omitempty"`
}
type routeOptions struct {
	Rules                 []route `json:"rules"`
	Final                 string  `json:"final,omitempty"`
	DefaultDomainResolver string  `json:"default_domain_resolver"`
}
type route struct {
	candidates map[string]collector.Exit `json:"-"`
	origin     collector.Exit            `json:"-"`
	Inbound    []string                  `json:"inbound,omitempty"`
	AuthUser   []string                  `json:"auth_user,omitempty"`
	Domain     []string                  `json:"domain,omitempty"`
	Suffix     []string                  `json:"domain_suffix,omitempty"`
	Regex      []string                  `json:"domain_regex,omitempty"`
	CIDR       []string                  `json:"ip_cidr,omitempty"`
	Ports      []uint16                  `json:"port,omitempty"`
	PortRange  []string                  `json:"port_range,omitempty"`
	Network    []string                  `json:"network,omitempty"`
	Action     string                    `json:"action"`
	Outbound   string                    `json:"outbound,omitempty"`
	Server     string                    `json:"server,omitempty"`
}

type renderOptions struct {
	statsPort         int
	statsSecret       string
	access            bool
	health            map[string]scheduler.Health
	probePorts        map[string]int
	probeFingerprints map[string]string
	wireGuardOutbound bool
	wireGuardInbound  bool
}

func Render(ctx context.Context, state desiredstate.State, at time.Time) (core.RenderedConfig, error) {
	return renderWithOptions(ctx, state, at, renderOptions{})
}

func renderWithOptions(ctx context.Context, state desiredstate.State, at time.Time, options renderOptions) (core.RenderedConfig, error) {
	if err := ctx.Err(); err != nil {
		return core.RenderedConfig{}, err
	}
	if err := state.Validate(); err != nil {
		return core.RenderedConfig{}, err
	}
	original := state
	state, err := state.RuntimeAt(at)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	original = state
	state, aliases, err := scheduling.State(state)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	credentials := map[string]desiredstate.Credential{}
	for _, credential := range state.Credentials {
		credentials[credential.ID] = credential
	}
	doc := document{Log: logOptions{Disabled: true}, Inbounds: []inbound{}, Outbounds: []outbound{}, Route: routeOptions{Rules: []route{}}}
	// Local name resolution follows the node's system DNS. localhost always
	// resolves to loopback, including hosts files with only commented examples.
	doc.DNS = dnsOptions{Servers: []dnsServer{{Type: "local", Tag: "_panel_dns_"}, {Type: "hosts", Tag: "_panel_loopback_", Predefined: map[string][]string{"localhost": {"127.0.0.1", "::1"}}}}, Final: "_panel_dns_"}
	doc.Route.DefaultDomainResolver = "_panel_dns_"
	protocols := map[string]string{}
	for _, entry := range state.Outbounds {
		if !entry.Enabled {
			continue
		}
		if entry.ID == "_panel_terminal_" || strings.HasPrefix(entry.ID, "_panel_access_rule_") || strings.HasPrefix(entry.ID, "_panel_group_") {
			return core.RenderedConfig{}, errors.New("sing-box reserved terminal tag cannot be used by business configuration")
		}
		protocols[entry.ID] = entry.Protocol
		if entry.Protocol == "block" {
			continue
		}
		if entry.Protocol == "wireguard" {
			if !options.wireGuardOutbound {
				return core.RenderedConfig{}, errors.New("sing-box WireGuard requires detected with_wireguard endpoint capability")
			}
			endpoint, err := compileWireGuardEndpoint(entry, credentials)
			if err != nil {
				return core.RenderedConfig{}, err
			}
			doc.Endpoints = append(doc.Endpoints, endpoint)
			continue
		}
		compiled := outbound{Type: entry.Protocol, Tag: entry.ID, Server: entry.Address, Port: entry.Port}
		if entry.Protocol == "socks5" {
			compiled.Type = "socks"
		}
		if err := supported(entry.Protocol); err != nil {
			return core.RenderedConfig{}, err
		}
		if entry.Protocol != "direct" {
			credential, exists := credentials[entry.CredentialID]
			if !exists && (entry.CredentialID != "" || entry.Protocol != "http" && entry.Protocol != "socks5") {
				return core.RenderedConfig{}, errors.New("outbound credential missing")
			}
			switch entry.Protocol {
			case "http", "socks5":
				compiled.Username = credential.Username
				compiled.Password = credential.Secret
			case "vless":
				compiled.UUID = credential.Secret
				compiled.Flow = entry.Settings.Flow
			case "tuic":
				compiled.UUID = credential.Username
				compiled.Password = credential.Secret
			default:
				compiled.Password = credential.Secret
			}
		}
		if q := entry.Settings.QUIC; q != nil {
			compiled.UpMbps = q.UpMbps
			compiled.DownMbps = q.DownMbps
			compiled.CongestionControl = q.CongestionControl
			compiled.UDPRelayMode = q.UDPRelayMode
		}
		if entry.Protocol == "shadowsocks" {
			compiled.Method = cipher(entry.Settings)
		}
		compiled.TLS, compiled.Transport, err = security(entry.Protocol, entry.Settings, false)
		if err != nil {
			return core.RenderedConfig{}, err
		}
		doc.Outbounds = append(doc.Outbounds, compiled)
	}
	// A reject rule represents Block on modern sing-box; a private Direct
	// terminal is unreachable because every enabled inbound has a terminal rule.
	doc.Outbounds = append(doc.Outbounds, outbound{Type: "direct", Tag: "_panel_terminal_"})
	doc.Route.Final = "_panel_terminal_"
	result := core.RenderedConfig{NodeID: state.NodeID, Revision: state.Revision}
	result.RuntimeAccountsComplete = true
	for _, entry := range state.Inbounds {
		if !entry.Enabled {
			continue
		}
		if entry.Protocol == "wireguard" && (!options.wireGuardInbound || options.statsPort == 0) {
			return core.RenderedConfig{}, errors.New("WireGuard inbound requires detected managed identity, exact listener and native account controls")
		}
		if len(entry.CredentialIDs) == 0 {
			result.RuntimeAccountsComplete = false
		}
		for _, id := range entry.CredentialIDs {
			credential := credentials[id]
			if !credential.Internal && (credential.UserID == "" || credential.DeviceID == "") {
				result.RuntimeAccountsComplete = false
			}
		}
	}
	result.ProbeFingerprints = options.probeFingerprints
	for _, entry := range state.Inbounds {
		if !entry.Enabled {
			continue
		}
		if entry.Protocol == "wireguard" {
			endpoint, err := compileWireGuardInbound(entry, credentials)
			if err != nil {
				return result, err
			}
			doc.Endpoints = append(doc.Endpoints, endpoint)
			result.UDPHealthEndpoints = append(result.UDPHealthEndpoints, net.JoinHostPort(entry.Listen, strconv.Itoa(entry.Port)))
			continue
		}
		if err := supported(entry.Protocol); err != nil {
			return result, err
		}
		compiled := inbound{Type: entry.Protocol, Tag: entry.ID, Listen: entry.Listen, Port: entry.Port}
		if entry.Protocol == "socks5" {
			compiled.Type = "socks"
		}
		for _, id := range entry.CredentialIDs {
			credential, exists := credentials[id]
			if !exists {
				return result, errors.New("inbound credential missing")
			}
			user := userOptions{}
			switch entry.Protocol {
			case "http", "socks5":
				user.Username = credential.Username
				user.Password = credential.Secret
			case "vless":
				user.Name = credential.ID
				user.UUID = credential.Secret
				user.Flow = entry.Settings.Flow
			case "trojan", "hysteria2":
				user.Name = credential.ID
				user.Password = credential.Secret
			case "tuic":
				user.Name = credential.ID
				user.UUID = credential.Username
				user.Password = credential.Secret
			case "shadowsocks":
				user.Name = credential.ID
				user.Password = credential.Secret
			}
			compiled.Users = append(compiled.Users, user)
		}
		if q := entry.Settings.QUIC; q != nil {
			compiled.UpMbps = q.UpMbps
			compiled.DownMbps = q.DownMbps
			compiled.CongestionControl = q.CongestionControl
		}
		if entry.Protocol == "shadowsocks" {
			compiled.Method = cipher(entry.Settings)
		}
		compiled.TLS, compiled.Transport, err = security(entry.Protocol, entry.Settings, true)
		if err != nil {
			return result, err
		}
		doc.Inbounds = append(doc.Inbounds, compiled)
		listen := entry.Listen
		if listen == "0.0.0.0" {
			listen = "127.0.0.1"
		}
		if listen == "::" {
			listen = "::1"
		}
		endpoint := net.JoinHostPort(listen, strconv.Itoa(entry.Port))
		if entry.Protocol == "hysteria2" || entry.Protocol == "tuic" {
			result.UDPHealthEndpoints = append(result.UDPHealthEndpoints, net.JoinHostPort(entry.Listen, strconv.Itoa(entry.Port)))
		} else {
			result.HealthEndpoints = append(result.HealthEndpoints, endpoint)
		}
	}
	targets, err := compileGroups(&doc, state, original, at, options, aliases)
	if err != nil {
		return result, err
	}
	for _, rule := range policy.Sort(state.Policies) {
		if !rule.Enabled || !policy.TimeActive(rule, at) || len(rule.Scope.NodeIDs) > 0 && !contains(rule.Scope.NodeIDs, state.NodeID) {
			continue
		}
		if options.access {
			// Runtime reconciliation fingerprints core JSON. Equal routing can
			// still change its rule identity or privacy setting at a time boundary.
			// An unused reserved tag makes that metadata change visible without
			// weakening match conditions or adding listeners/control operations.
			logging := "1"
			if rule.Logging != nil && !rule.Logging.LogDomain {
				logging = "0"
			}
			doc.Outbounds = append(doc.Outbounds, outbound{Type: "direct", Tag: "_panel_access_rule_" + rule.ID + "_" + logging})
		}
		if len(rule.Scope.GroupIDs) > 0 || len(rule.Match.ServiceIDs) > 0 {
			return result, errors.New("services and node groups must be expanded before sing-box compilation")
		}
		target := rule.Action.TargetID
		if rule.Action.Type == "Direct" || rule.Action.Type == "Block" {
			kind := strings.ToLower(rule.Action.Type)
			target = ""
			for _, entry := range state.Outbounds {
				if entry.Enabled && entry.Protocol == kind {
					target = entry.ID
					break
				}
			}
		} else if rule.Action.Type != "Outbound" && rule.Action.Type != "OutboundGroup" && rule.Action.Type != "ProxyChain" {
			return result, errors.New("sing-box policy scheduling target is not available")
		}
		chosen, exists := targets[normalizedTarget(target)]
		if !exists {
			return result, errors.New("sing-box policy target unavailable")
		}
		base := selectedRoute(chosen)
		base.origin.RuleID = rule.ID
		if rule.Logging != nil {
			base.origin.LogDomain = rule.Logging.LogDomain
		}
		if base.candidates != nil {
			candidates := map[string]collector.Exit{}
			for id, origin := range base.candidates {
				origin.RuleID = rule.ID
				origin.LogDomain = base.origin.LogDomain
				candidates[id] = origin
			}
			base.candidates = candidates
		}
		base.Domain = rule.Match.Domains
		base.Suffix = rule.Match.Suffixes
		base.CIDR = rule.Match.CIDRs
		base.Domain = nil
		for _, domain := range rule.Match.Domains {
			if strings.Contains(domain, "*") {
				base.Regex = append(base.Regex, policy.Pattern(domain))
			} else {
				base.Domain = append(base.Domain, domain)
			}
		}
		if rule.Match.Network != "" && rule.Match.Network != "all" {
			base.Network = []string{rule.Match.Network}
		}
		for _, port := range rule.Match.Ports {
			if strings.Contains(port, "-") {
				base.PortRange = append(base.PortRange, strings.ReplaceAll(port, "-", ":"))
			} else {
				number, err := strconv.Atoi(port)
				if err != nil {
					return result, err
				}
				base.Ports = append(base.Ports, uint16(number))
			}
		}
		if len(rule.Scope.UserIDs) == 0 && len(rule.Scope.DeviceIDs) == 0 {
			appendRoute(&doc, base)
			continue
		}
		for _, entry := range state.Inbounds {
			if !entry.Enabled {
				continue
			}
			if entry.Protocol == "socks5" && rule.Match.Network != "tcp" {
				return result, errors.New("sing-box scoped SOCKS5 UDP identity requires native validation before publication")
			}
			scoped := base
			scoped.Inbound = []string{entry.ID}
			for _, id := range entry.CredentialIDs {
				credential := credentials[id]
				if len(rule.Scope.UserIDs) > 0 && !contains(rule.Scope.UserIDs, credential.UserID) || len(rule.Scope.DeviceIDs) > 0 && !contains(rule.Scope.DeviceIDs, credential.DeviceID) {
					continue
				}
				name := credential.ID
				if entry.Protocol == "http" || entry.Protocol == "socks5" {
					name = credential.Username
				}
				scoped.AuthUser = append(scoped.AuthUser, name)
			}
			if len(scoped.AuthUser) > 0 {
				appendRoute(&doc, scoped)
			}
		}
	}
	for _, entry := range state.Inbounds {
		if entry.Enabled {
			chosen, exists := targets[normalizedTarget(entry.DefaultOutbound)]
			if !exists {
				return result, errors.New("sing-box default scheduler target unavailable")
			}
			rule := selectedRoute(chosen)
			rule.Inbound = []string{entry.ID}
			appendRoute(&doc, rule)
		}
	}
	if err := addProbeInbounds(&doc, state, options, &result); err != nil {
		return result, err
	}
	if options.statsPort > 0 {
		if err := configureStats(&doc, state, options.statsPort, options.statsSecret, &result); err != nil {
			return result, err
		}
	}
	if options.access {
		result.AccessContext = collector.NewContext(state)
		result.AccessContext.Selections = map[string]map[string]collector.Exit{}
		for index, compiled := range doc.Route.Rules {
			result.AccessContext.Exits[collector.SingboxRuleTag(index)] = compiled.origin
			if compiled.candidates != nil {
				result.AccessContext.Selections[collector.SingboxRuleTag(index)] = compiled.candidates
			}
		}
		doc.Log = logOptions{Level: "debug", Output: "stdout", Timestamp: true}
	}
	if err := applyOutboundSource(&doc, state); err != nil {
		return result, err
	}
	result.Data, err = json.MarshalIndent(doc, "", "  ")
	return result, err
}

func appendRoute(doc *document, terminal route) {
	// Resolve a loopback hostname only for a local Direct route. Forwarding
	// protocols must retain the original domain for the next node's policies.
	if terminal.origin.Protocol == "direct" {
		resolver := terminal
		resolver.Domain, resolver.Suffix, resolver.Regex = []string{"localhost"}, nil, nil
		resolver.Action, resolver.Outbound, resolver.Server = "resolve", "", "_panel_loopback_"
		resolver.origin = collector.Exit{Protocol: "_resolve_", ID: "_panel_loopback_"}
		doc.Route.Rules = append(doc.Route.Rules, resolver)
	}
	doc.Route.Rules = append(doc.Route.Rules, terminal)
}

func supported(protocol string) error {
	switch protocol {
	case "direct", "block", "http", "socks5", "shadowsocks", "vless", "trojan", "hysteria2", "tuic":
		return nil
	}
	return errors.New("protocol has no verified sing-box adapter")
}
func cipher(settings desiredstate.ProtocolSettings) string {
	if settings.Method == "" {
		return "chacha20-ietf-poly1305"
	}
	return settings.Method
}
func contains(values []string, value string) bool {
	for _, entry := range values {
		if entry == value {
			return true
		}
	}
	return false
}
func security(protocol string, settings desiredstate.ProtocolSettings, inbound bool) (*tlsOptions, *transportOptions, error) {
	if protocol == "direct" || protocol == "block" {
		return nil, nil, nil
	}
	if settings.WireGuard != nil {
		return nil, nil, errors.New("unexpected WireGuard settings")
	}
	var tls *tlsOptions
	if settings.Security == "tls" {
		if protocol == "socks5" || protocol == "shadowsocks" {
			return nil, nil, errors.New("sing-box protocol does not support configured TLS layer")
		}
		tls = &tlsOptions{Enabled: true, ServerName: settings.ServerName}
		if protocol == "hysteria2" || protocol == "tuic" {
			tls.ALPN = []string{"h3"}
		}
		if inbound {
			tls.Certificate = strings.Split(strings.TrimSpace(settings.CertificatePEM), "\n")
			tls.Key = strings.Split(strings.TrimSpace(settings.PrivateKeyPEM), "\n")
		} else if settings.TrustedCAPEM != "" {
			tls.Certificate = strings.Split(strings.TrimSpace(settings.TrustedCAPEM), "\n")
		}
	}
	var transport *transportOptions
	if settings.Transport != "" && settings.Transport != "tcp" && settings.Transport != "quic" {
		if protocol != "vless" && protocol != "trojan" {
			return nil, nil, errors.New("sing-box transport requires VLESS or Trojan")
		}
		transport = &transportOptions{Type: settings.Transport, Path: settings.Path, ServiceName: settings.ServiceName}
	}
	return tls, transport, nil
}

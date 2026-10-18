package xray

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"time"
)

type configDocument struct {
	accessOrigins map[string]collector.Exit
	Log           logConfig          `json:"log"`
	Inbounds      []inboundConfig    `json:"inbounds"`
	Outbounds     []outboundConfig   `json:"outbounds"`
	Routing       routingConfig      `json:"routing"`
	Observatory   *observatoryConfig `json:"observatory,omitempty"`
	API           *statsAPIConfig    `json:"api,omitempty"`
	Stats         *struct{}          `json:"stats,omitempty"`
	Policy        *statsPolicyConfig `json:"policy,omitempty"`
}
type logConfig struct {
	Access string `json:"access"`
	Error  string `json:"error"`
	Level  string `json:"loglevel"`
}
type inboundConfig struct {
	Tag      string          `json:"tag"`
	Listen   string          `json:"listen"`
	Port     int             `json:"port"`
	Protocol string          `json:"protocol"`
	Settings any             `json:"settings"`
	Stream   *streamSettings `json:"streamSettings,omitempty"`
	Sniffing sniffing        `json:"sniffing"`
}
type socksSettings struct {
	Auth     string    `json:"auth,omitempty"`
	Accounts []account `json:"accounts,omitempty"`
	UDP      bool      `json:"udp"`
}
type account struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}
type sniffing struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
	RouteOnly    bool     `json:"routeOnly"`
}
type outboundConfig struct {
	Settings    any             `json:"settings,omitempty"`
	Stream      *streamSettings `json:"streamSettings,omitempty"`
	Tag         string          `json:"tag"`
	Protocol    string          `json:"protocol"`
	SendThrough string          `json:"sendThrough,omitempty"`
}
type routingConfig struct {
	DomainStrategy string           `json:"domainStrategy"`
	Rules          []route          `json:"rules"`
	Balancers      []balancerConfig `json:"balancers,omitempty"`
}
type route struct {
	Type        string   `json:"type"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Port        string   `json:"port,omitempty"`
	Network     string   `json:"network,omitempty"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	BalancerTag string   `json:"balancerTag,omitempty"`
	RuleTag     string   `json:"ruleTag,omitempty"`
	User        []string `json:"user,omitempty"`
	Source      []string `json:"source,omitempty"`
}

func Render(ctx context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	return RenderAt(ctx, state, time.Now().UTC())
}
func RenderAt(ctx context.Context, state desiredstate.State, at time.Time) (core.RenderedConfig, error) {
	return renderWithOptions(ctx, state, at, RenderOptions{})
}
func renderWithOptions(ctx context.Context, state desiredstate.State, at time.Time, options RenderOptions) (core.RenderedConfig, error) {
	if err := ctx.Err(); err != nil {
		return core.RenderedConfig{}, err
	}
	if err := state.Validate(); err != nil {
		return core.RenderedConfig{}, err
	}
	state, err := state.RuntimeAt(at)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	original := state
	state, aliases, err := schedulingState(state)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	document := configDocument{Log: logConfig{Access: "none", Error: "none", Level: "none"}, Inbounds: []inboundConfig{}, Outbounds: []outboundConfig{}, Routing: routingConfig{DomainStrategy: "AsIs", Rules: []route{}}}
	if options.Access {
		if options.AccessLogPath == "" {
			return core.RenderedConfig{}, errors.New("Xray access logging requires an agent-owned access log path")
		}
		// The core appends to the Agent's own access log file; the Agent tails
		// it, empties it as soon as it has read it and truncates it at its cap
		// (accessLogLimit), so a URL path never stays on disk.
		document.Log.Access = options.AccessLogPath
		document.Log.Level = "error"
		document.accessOrigins = map[string]collector.Exit{}
	}
	credentials := map[string]desiredstate.Credential{}
	for _, credential := range state.Credentials {
		credentials[credential.ID] = credential
	}

	for _, outbound := range state.Outbounds {
		if !outbound.Enabled {
			continue
		}
		compiled, err := buildOutbound(outbound, credentials)
		if err != nil {
			return core.RenderedConfig{}, err
		}
		document.Outbounds = append(document.Outbounds, compiled)
		if options.Access {
			document.accessOrigins[outbound.ID] = accessOrigin(original, outbound)
		}

	}
	rendered := core.RenderedConfig{NodeID: state.NodeID, Revision: state.Revision, HealthEndpoints: []string{}}
	if options.Access {
		rendered.AccessContext = collector.NewContext(original)
		rendered.AccessLogPath = options.AccessLogPath
	}
	for _, inbound := range state.Inbounds {
		if !inbound.Enabled {
			continue
		}
		protocol, settings, transport, err := buildInbound(inbound, credentials)
		if err != nil {
			return rendered, err
		}
		document.Inbounds = append(document.Inbounds, inboundConfig{Tag: inbound.ID, Listen: inbound.Listen, Port: inbound.Port, Protocol: protocol, Settings: settings, Stream: transport, Sniffing: sniffing{Enabled: inbound.Protocol != "wireguard", DestOverride: []string{"http", "tls"}, RouteOnly: true}})
		address := inbound.Listen
		if address == "0.0.0.0" {
			address = "127.0.0.1"
		}
		if address == "::" {
			address = "::1"
		}
		endpoint := net.JoinHostPort(address, strconv.Itoa(inbound.Port))
		if inbound.Protocol == "wireguard" {
			rendered.UDPHealthEndpoints = append(rendered.UDPHealthEndpoints, endpoint)
		} else {
			rendered.HealthEndpoints = append(rendered.HealthEndpoints, endpoint)
		}
	}
	if len(document.Outbounds) == 0 {
		return rendered, errors.New("configuration must include an enabled outbound")
	}
	targets, err := compileGroups(&document, state, at, options, aliases)
	if err != nil {
		return rendered, err
	}
	compiledPolicies, err := compilePolicies(state, at)
	if err != nil {
		return rendered, err
	}
	for index := range compiledPolicies {
		key := normalizedTarget(compiledPolicies[index].OutboundTag)
		selected, ok := targets[key]
		if !ok {
			return rendered, errors.New("policy target unavailable")
		}
		assignTarget(&compiledPolicies[index], selected)
	}
	for _, rule := range compiledPolicies {
		if options.Access {
			selected := target{Outbound: rule.OutboundTag, Balancer: rule.BalancerTag}
			rules, err := accessAccountRules(&document, rule, selected, original)
			if err != nil {
				return rendered, err
			}
			document.Routing.Rules = append(document.Routing.Rules, rules...)
		} else {
			document.Routing.Rules = append(document.Routing.Rules, rule)
		}
	}
	for _, inbound := range state.Inbounds {
		if inbound.Enabled {
			selected, ok := targets[normalizedTarget(inbound.DefaultOutbound)]
			if !ok {
				return rendered, errors.New("default target unavailable")
			}
			rule := route{Type: "field", InboundTag: []string{inbound.ID}}
			assignTarget(&rule, selected)
			if options.Access {
				rules, err := accessAccountRules(&document, rule, selected, original)
				if err != nil {
					return rendered, err
				}
				document.Routing.Rules = append(document.Routing.Rules, rules...)
			} else {
				document.Routing.Rules = append(document.Routing.Rules, rule)
			}
		}
	}
	if err := addProbeInbounds(&document, state, options, &rendered); err != nil {
		return rendered, err
	}
	if options.Access {
		rendered.AccessContext.Exits = document.accessOrigins
	}
	if options.StatsPort > 0 {
		if err := configureStats(&document, original, options.StatsPort, options, &rendered); err != nil {
			return rendered, err
		}
	}
	if err := applyOutboundSource(&document, state.Network); err != nil {
		return rendered, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	rendered.Data = data
	return rendered, err
}

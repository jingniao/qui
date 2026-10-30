package xray

import (
	"context"
	"errors"
	"fmt"
	"net"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"sort"
	"strconv"
	"strings"
	"time"
)

type RenderOptions struct {
	PrivatePorts  []int
	Health        map[string]scheduler.Health
	ProbePorts    map[string]int
	Access        bool
	AccessLogPath string
	StatsPort     int
	OnlineStats   bool
}
type balancerConfig struct {
	Tag         string           `json:"tag"`
	Selector    []string         `json:"selector"`
	FallbackTag string           `json:"fallbackTag"`
	Strategy    balancerStrategy `json:"strategy"`
}
type balancerStrategy struct {
	Type string `json:"type"`
}
type observatoryConfig struct {
	SubjectSelector []string `json:"subjectSelector"`
	ProbeURL        string   `json:"probeUrl,omitempty"`
	ProbeInterval   string   `json:"probeInterval"`
}
type target struct{ Outbound, Balancer string }

func normalizedTarget(value string) string {
	value = strings.TrimPrefix(value, "group:")
	return strings.TrimPrefix(value, "chain:")
}
func assignTarget(rule *route, selected target) {
	rule.OutboundTag = selected.Outbound
	rule.BalancerTag = selected.Balancer
}
func compileGroups(document *configDocument, state desiredstate.State, at time.Time, options RenderOptions, aliases map[string]string) (map[string]target, error) {
	targets := map[string]target{}
	compiled := map[string]outboundConfig{}
	for _, out := range document.Outbounds {
		compiled[out.Tag] = out
	}
	groups := map[string]desiredstate.OutboundGroup{}
	chains := map[string]desiredstate.ProxyChain{}
	available := map[string]bool{}
	for id := range compiled {
		available[id] = true
	}
	for _, group := range state.OutboundGroups {
		groups[group.ID] = group
		available[group.ID] = true
	}
	for _, chain := range state.ProxyChains {
		if len(chain.NodeIDs) > 0 && chain.NodeIDs[0] == state.NodeID {
			chains[chain.ID] = chain
			available[chain.ID] = true
		}
	}
	visiting := map[string]bool{}
	observatoryTags := []string{}
	observatoryURL := ""
	observatoryInterval := 0
	var resolve func(string) (target, error)
	resolve = func(id string) (target, error) {
		if ready, ok := targets[id]; ok {
			return ready, nil
		}
		if visiting[id] {
			return target{}, errors.New("fallback scheduler cycle")
		}
		visiting[id] = true
		defer delete(visiting, id)
		remember := func(value target) (target, error) { targets[id] = value; return value, nil }
		if alias, ok := aliases[id]; ok {
			value, err := resolve(alias)
			if err != nil {
				return target{}, err
			}
			return remember(value)
		}
		if group, ok := groups[id]; ok {
			if err := scheduler.ValidateGroup(group, available); err != nil {
				return target{}, err
			}
			fallbackID := "block"
			if group.Failover == "Direct" {
				fallbackID = "direct"
			} else if desiredstate.BackupKind(group.Failover) != "" {
				fallbackID = group.BackupTargetID
			}
			fallback, err := resolve(fallbackID)
			if err != nil {
				return target{}, err
			}
			members := scheduler.HealthyMembers(group, options.Health, at)
			if len(members) == 0 {
				return remember(fallback)
			}
			if group.Strategy == "primary" || group.Strategy == "latency" {
				// Upstream Xray has no priority-ordered or "first healthy"
				// balancer strategy, and a balancer would make the recorded
				// configured egress dynamic. The Agent already resolves the
				// first healthy member by priority (primary) or by measured
				// latency (latency, which is leastPing semantics) with its own
				// probes and renders that member's outbound directly.
				return remember(target{Outbound: members[0].OutboundID})
			}
			// round_robin: upstream balancer over the healthy members, with the
			// group's failure policy as fallbackTag. Each member is cloned so
			// the core's own access log names the member it selected and the
			// record stays exact.
			selectors := []string{}
			for index, member := range members {
				out, ok := compiled[member.OutboundID]
				if !ok {
					return target{}, errors.New("group member unavailable")
				}
				out.Tag = fmt.Sprintf("_group_%s_%03d_", group.ID, index)
				if document.accessOrigins != nil {
					document.accessOrigins[out.Tag] = document.accessOrigins[member.OutboundID]
				}
				document.Outbounds = append(document.Outbounds, out)
				selectors = append(selectors, out.Tag)
			}
			fallbackTag := fallback.Outbound
			if fallbackTag == "" {
				fallbackTag = "block"
			}
			tag := "_balancer_" + group.ID
			document.Routing.Balancers = append(document.Routing.Balancers, balancerConfig{Tag: tag, Selector: selectors, FallbackTag: fallbackTag, Strategy: balancerStrategy{Type: "roundRobin"}})
			// Upstream allows one observatory per configuration. It probes the
			// cloned members so the balancer can skip one that fails between
			// Agent re-renders; groups with different probe settings share the
			// first balancer group's URL and interval.
			observatoryTags = append(observatoryTags, selectors...)
			if observatoryURL == "" {
				observatoryURL, observatoryInterval = group.ProbeURL, group.ProbeInterval
			}
			return remember(target{Balancer: tag})
		}
		if chain, ok := chains[id]; ok {
			if len(chain.OutboundIDs) == 0 {
				return target{}, errors.New("chain entry outbound unavailable")
			}
			if _, ok := compiled[chain.OutboundIDs[0]]; !ok {
				return target{}, errors.New("chain entry outbound missing")
			}
			// Chain-specific failure handling is evaluated at entry. Individual hop
			// fallback must not change the path used to test or carry this chain.
			return remember(target{Outbound: chain.OutboundIDs[0]})
		}
		if _, ok := compiled[id]; ok {
			return remember(target{Outbound: id})
		}
		return target{}, errors.New("fallback target unavailable")
	}
	orderedIDs := make([]string, 0, len(available))
	for id := range available {
		orderedIDs = append(orderedIDs, id)
	}
	sort.Strings(orderedIDs)
	for _, id := range orderedIDs {
		if _, err := resolve(id); err != nil {
			return nil, err
		}
	}
	if len(observatoryTags) > 0 {
		sort.Strings(observatoryTags)
		document.Observatory = &observatoryConfig{SubjectSelector: observatoryTags, ProbeURL: observatoryURL, ProbeInterval: strconv.Itoa(observatoryInterval) + "s"}
	}
	return targets, nil
}
func addProbeInbounds(document *configDocument, state desiredstate.State, options RenderOptions, rendered *core.RenderedConfig) error {
	urls := map[string]string{}
	intervals := map[string]int{}
	for _, group := range state.OutboundGroups {
		for _, member := range group.Members {
			if previous, exists := urls[member.OutboundID]; exists && previous != group.ProbeURL {
				return errors.New("an outbound cannot use conflicting probe URLs in different groups")
			}
			urls[member.OutboundID] = group.ProbeURL
			if value := intervals[member.OutboundID]; value == 0 || group.ProbeInterval < value {
				intervals[member.OutboundID] = group.ProbeInterval
			}
		}
	}
	ids := []string{}
	for id := range urls {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	probeRules := []route{}
	for _, id := range ids {
		port := options.ProbePorts[id]
		if port == 0 {
			continue
		}
		tag := "_probe_" + id
		document.Inbounds = append(document.Inbounds, inboundConfig{Tag: tag, Listen: "127.0.0.1", Port: port, Protocol: "socks", Settings: socksSettings{Auth: "noauth"}, Sniffing: sniffing{Enabled: false}})
		probeRules = append(probeRules, route{Type: "field", InboundTag: []string{tag}, OutboundTag: id})
		endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		rendered.HealthEndpoints = append(rendered.HealthEndpoints, endpoint)
		rendered.Probes = append(rendered.Probes, core.Probe{OutboundID: id, Address: endpoint, URL: urls[id], Interval: time.Duration(intervals[id]) * time.Second})
	}
	document.Routing.Rules = append(probeRules, document.Routing.Rules...)
	return nil
}
func (a *Adapter) options(ctx context.Context, state desiredstate.State) (RenderOptions, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.probePorts == nil {
		a.probePorts = map[string]int{}
	}
	if a.outboundHealth == nil {
		a.outboundHealth = map[string]scheduler.Health{}
	}
	for _, group := range state.OutboundGroups {
		for _, member := range group.Members {
			if a.probePorts[member.OutboundID] != 0 {
				continue
			}
			port, err := a.privatePortLocked(state)
			if err != nil {
				return RenderOptions{}, err
			}
			a.probePorts[member.OutboundID] = port
		}
	}
	health := map[string]scheduler.Health{}
	ports := map[string]int{}
	for id, value := range a.outboundHealth {
		health[id] = value
	}
	for id, value := range a.probePorts {
		ports[id] = value
	}
	return RenderOptions{Health: health, ProbePorts: ports}, ctx.Err()
}

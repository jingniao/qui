package singbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"sort"
	"strconv"
	"strings"
	"time"
)

type selection struct {
	tag        string
	origin     collector.Exit
	candidates map[string]collector.Exit
}

func normalizedTarget(value string) string {
	return strings.TrimPrefix(strings.TrimPrefix(value, "group:"), "chain:")
}

func outboundOrigin(original desiredstate.State, entry desiredstate.Outbound) collector.Exit {
	id := entry.ID
	for _, rule := range original.Policies {
		if id == "_policy_raw_"+rule.ID {
			id = rule.Action.TargetID
			break
		}
	}
	return collector.Exit{ID: id, NativeTag: entry.ID, Protocol: entry.Protocol, LogDomain: true}
}

func compileGroups(doc *document, state, original desiredstate.State, at time.Time, options renderOptions, aliases map[string]string) (map[string]selection, error) {
	compiled := map[string]outbound{}
	origins := map[string]collector.Exit{}
	available := map[string]bool{}
	for _, entry := range doc.Outbounds {
		compiled[entry.Tag] = entry
		available[entry.Tag] = true
	}
	for _, entry := range doc.Endpoints {
		compiled[entry.Tag] = outbound{Type: entry.Type, Tag: entry.Tag}
		available[entry.Tag] = true
	}
	for _, entry := range state.Outbounds {
		if entry.Enabled {
			origins[entry.ID] = outboundOrigin(original, entry)
			available[entry.ID] = true
		}
	}
	groups := map[string]desiredstate.OutboundGroup{}
	chains := map[string]desiredstate.ProxyChain{}
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
	terminal := func(protocol string) (selection, error) {
		for _, entry := range state.Outbounds {
			if entry.Enabled && entry.Protocol == protocol {
				return selection{tag: entry.ID, origin: origins[entry.ID]}, nil
			}
		}
		return selection{}, errors.New("scheduler terminal outbound unavailable")
	}
	targets := map[string]selection{}
	visiting := map[string]bool{}
	var resolve func(string) (selection, error)
	resolve = func(id string) (selection, error) {
		if ready, ok := targets[id]; ok {
			return ready, nil
		}
		if visiting[id] {
			return selection{}, errors.New("sing-box scheduler cycle")
		}
		visiting[id] = true
		defer delete(visiting, id)
		remember := func(value selection) (selection, error) { targets[id] = value; return value, nil }
		if alias, ok := aliases[id]; ok {
			value, err := resolve(alias)
			if err != nil {
				return selection{}, err
			}
			return remember(value)
		}
		if group, ok := groups[id]; ok {
			if err := scheduler.ValidateGroup(group, available); err != nil {
				return selection{}, err
			}
			var fallback selection
			var err error
			if desiredstate.BackupKind(group.Failover) != "" {
				fallback, err = resolve(group.BackupTargetID)
			} else if group.Failover == "Direct" {
				fallback, err = terminal("direct")
			} else {
				fallback, err = terminal("block")
			}
			if err != nil {
				return selection{}, err
			}
			members := scheduler.HealthyMembers(group, options.health, at)
			if len(members) == 0 {
				return remember(fallback)
			}
			if group.Strategy == "primary" {
				member := members[0].OutboundID
				return remember(selection{tag: member, origin: origins[member]})
			}
			// sing-box has no priority-ordered or round-robin scheduler (plan
			// D4): upstream only offers the latency-tested urltest group. Both
			// remaining strategies render that upstream construct with the
			// group's own probe URL and interval, so the core keeps its
			// documented behaviour. The selected member is not known before the
			// core picks it, so access records resolve it from the member set
			// the core's own outbound log line names.
			listed := []string{}
			candidates := map[string]collector.Exit{}
			for _, member := range members {
				if _, ok := compiled[member.OutboundID]; !ok {
					return selection{}, errors.New("group member outbound unavailable")
				}
				listed = append(listed, member.OutboundID)
				candidates[member.OutboundID] = origins[member.OutboundID]
			}
			tag := "_panel_group_" + group.ID
			doc.Outbounds = append(doc.Outbounds, outbound{Type: "urltest", Tag: tag, Outbounds: listed, URL: group.ProbeURL, Interval: strconv.Itoa(group.ProbeInterval) + "s"})
			return remember(selection{tag: tag, origin: collector.Exit{ID: group.ID, NativeTag: tag, Protocol: collector.EgressGroupProtocol, LogDomain: true}, candidates: candidates})
		}
		if chain, ok := chains[id]; ok {
			if len(chain.OutboundIDs) == 0 {
				return selection{}, errors.New("chain entry outbound unavailable")
			}
			first := chain.OutboundIDs[0]
			if _, ok := compiled[first]; !ok {
				return selection{}, errors.New("chain entry outbound missing")
			}
			origin := origins[first]
			origin.ChainID = chain.ID
			for _, approved := range original.ProxyChains {
				for _, rule := range original.Policies {
					if chain.ID == "_policy_chain_"+rule.ID && approved.ID == rule.Action.TargetID {
						origin.ChainID = approved.ID
					}
				}
			}
			return remember(selection{tag: first, origin: origin})
		}
		if origin, ok := origins[id]; ok && origin.Protocol == "block" {
			return remember(selection{tag: id, origin: origin})
		}
		if _, ok := compiled[id]; ok {
			return remember(selection{tag: id, origin: origins[id]})
		}
		return selection{}, errors.New("sing-box scheduler target unavailable")
	}
	ids := make([]string, 0, len(available))
	for id := range available {
		if id != "_panel_terminal_" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := resolve(id); err != nil {
			return nil, err
		}
	}
	return targets, nil
}

func selectedRoute(value selection) route {
	if value.origin.Protocol == "block" {
		return route{Action: "reject", origin: value.origin}
	}
	return route{Action: "route", Outbound: value.tag, origin: value.origin, candidates: value.candidates}
}

func probesFor(state desiredstate.State) (map[string]core.Probe, error) {
	probes := map[string]core.Probe{}
	for _, group := range state.OutboundGroups {
		for _, member := range group.Members {
			probe, exists := probes[member.OutboundID]
			if exists && probe.URL != group.ProbeURL {
				return nil, errors.New("outbound has conflicting probe URLs")
			}
			if !exists || time.Duration(group.ProbeInterval)*time.Second < probe.Interval {
				probe = core.Probe{OutboundID: member.OutboundID, URL: group.ProbeURL, Interval: time.Duration(group.ProbeInterval) * time.Second}
			}
			probes[member.OutboundID] = probe
		}
	}
	return probes, nil
}

func probeFingerprint(state desiredstate.State, probe core.Probe) string {
	var entry desiredstate.Outbound
	for _, out := range state.Outbounds {
		if out.ID == probe.OutboundID {
			entry = out
			break
		}
	}
	credentials := []desiredstate.Credential{}
	for _, credential := range state.Credentials {
		if credential.ID == entry.CredentialID {
			credentials = append(credentials, credential)
		}
	}
	data, _ := json.Marshal(struct {
		Entry       desiredstate.Outbound
		Credentials []desiredstate.Credential
		URL         string
	}{entry, credentials, probe.URL})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func addProbeInbounds(doc *document, state desiredstate.State, options renderOptions, result *core.RenderedConfig) error {
	probes, err := probesFor(state)
	if err != nil {
		return err
	}
	ids := []string{}
	for id := range probes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rules := []route{}
	privatePorts := []uint16{}
	for _, id := range ids {
		port := options.probePorts[id]
		if port == 0 {
			continue
		}
		if port < 1 || port > 65535 {
			return errors.New("invalid private probe port")
		}
		for _, entry := range doc.Inbounds {
			if entry.Port == port {
				return errors.New("private probe port conflicts with listener")
			}
		}
		privatePorts = append(privatePorts, uint16(port))
		tag := "_panel_probe_" + id
		doc.Inbounds = append(doc.Inbounds, inbound{Type: "socks", Tag: tag, Listen: "127.0.0.1", Port: port})
		// Always probe the raw protocol path, never its Direct/fallback alias.
		rule := route{Inbound: []string{tag}, Action: "route", Outbound: id}
		for _, entry := range state.Outbounds {
			if entry.ID == id && entry.Protocol == "direct" {
				rules = append(rules, route{Inbound: []string{tag}, Domain: []string{"localhost"}, Action: "resolve", Server: "_panel_loopback_"})
				break
			}
		}
		rules = append(rules, rule)
		endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		result.HealthEndpoints = append(result.HealthEndpoints, endpoint)
		probe := probes[id]
		probe.Address = endpoint
		result.Probes = append(result.Probes, probe)
	}
	doc.Route.Rules = append(rules, doc.Route.Rules...)
	if len(privatePorts) > 0 {
		doc.Route.Rules = append([]route{{Ports: privatePorts, Action: "reject", origin: collector.Exit{Protocol: "block", LogDomain: false}}}, doc.Route.Rules...)
	}
	return nil
}

func (a *Adapter) Probe(ctx context.Context) error {
	a.mu.Lock()
	probes := append([]core.Probe(nil), a.current.Probes...)
	epoch := a.statsEpoch
	known := map[string]scheduler.Health{}
	for id, health := range a.outboundHealth {
		known[id] = health
	}
	a.mu.Unlock()
	now := time.Now().UTC()
	var first error
	for _, probe := range probes {
		if previous, ok := known[probe.OutboundID]; ok && now.Sub(previous.CheckedAt) < probe.Interval {
			continue
		}
		status, err := core.ObserveProbe(ctx, probe)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.mu.Lock()
		if a.statsEpoch == epoch && a.process != nil {
			if a.outboundHealth == nil {
				a.outboundHealth = map[string]scheduler.Health{}
			}
			a.outboundHealth[probe.OutboundID] = status
		}
		a.mu.Unlock()
		if err != nil && first == nil {
			first = errors.New("one or more core outbound probes failed")
		}
	}
	return first
}

func (a *Adapter) OutboundHealth() map[string]scheduler.Health {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := map[string]scheduler.Health{}
	for _, probe := range a.current.Probes {
		if status, ok := a.outboundHealth[probe.OutboundID]; ok {
			result[probe.OutboundID] = status
		}
	}
	return result
}

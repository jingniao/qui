package policy

import (
	"context"
	"errors"
	"fmt"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
	shared "proxycontrol/pkg/contracts/policy"
	"strings"
)

type Nodes interface {
	Get(context.Context, string) (node.Node, error)
	Group(context.Context, string) (node.Group, error)
	GroupIDsForNode(context.Context, string) ([]string, error)
	LiveIDs(context.Context) ([]string, error)
}
type Users interface {
	Get(context.Context, string) (user.User, error)
	Device(context.Context, string) (user.Device, error)
}
type Proxies interface {
	Outbound(context.Context, string, bool) (proxy.Outbound, error)
	Inbound(context.Context, string, bool) (proxy.Inbound, error)
	Inbounds(context.Context, string, int, int, bool) ([]proxy.Inbound, int, error)
	Group(context.Context, string) (proxy.Group, error)
	Chain(context.Context, string) (proxy.Chain, error)
}
type Settings interface {
	ReadSetup(context.Context) (string, string, error)
}
type Service struct {
	Repository       *Repository
	Nodes            Nodes
	Users            Users
	Proxies          Proxies
	Settings         Settings
	NodeCapabilities func(context.Context, string) ([]desiredstate.CoreKeywordCapability, error)
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func (s *Service) Save(ctx context.Context, ruleID string, input Input) (Record, error) {
	for _, id := range input.Scope.UserIDs {
		if _, err := s.Users.Get(ctx, id); err != nil {
			return Record{}, errors.New("policy user not found")
		}
	}
	// v1.1 scopes policies per user: a user has one hidden access identity,
	// so a user scope is exactly what a device scope was (migration 0113
	// rewrote the stored ones).
	if len(input.Scope.DeviceIDs) > 0 {
		return Record{}, errors.New("policy device scope was removed in v1.1; scope the policy to users")
	}
	for _, id := range input.Scope.NodeIDs {
		if n, err := s.Nodes.Get(ctx, id); err != nil || n.DeletedAt != "" {
			return Record{}, errors.New("policy node not found")
		}
	}
	for _, id := range input.Scope.GroupIDs {
		if _, err := s.Nodes.Group(ctx, id); err != nil {
			return Record{}, errors.New("policy node group not found")
		}
	}
	for _, id := range input.Match.ServiceIDs {
		if _, err := s.Repository.Service(ctx, id); err != nil {
			return Record{}, errors.New("service rule not found")
		}
	}
	if input.Timezone == "" {
		_, zone, err := s.Settings.ReadSetup(ctx)
		if err != nil {
			return Record{}, err
		}
		input.Timezone = zone
	}
	if input.Action.Failover == "" {
		input.Action.Failover = "Block"
	}
	if input.Action.Type == "Outbound" || input.Action.Type == "OutboundGroup" || input.Action.Type == "ProxyChain" {
		owner, err := s.validateTarget(ctx, input.Action.Type, input.Action.TargetID, input.Match.Network)
		if err != nil {
			return Record{}, err
		}
		if len(input.Scope.NodeIDs) == 0 {
			input.Scope.NodeIDs = []string{owner}
		} else if len(input.Scope.NodeIDs) != 1 || input.Scope.NodeIDs[0] != owner {
			return Record{}, errors.New("proxy target policy must apply to its owning entry node")
		}
		if err := s.validateActionBackup(ctx, owner, input.Action.Failover, input.Action.BackupID, input.Match.Network); err != nil {
			return Record{}, err
		}
	}
	if s.NodeCapabilities != nil {
		hasKeywords := len(input.Match.Keywords) > 0
		if !hasKeywords {
			for _, sID := range input.Match.ServiceIDs {
				if svc, err := s.Repository.Service(ctx, sID); err == nil && svc.Enabled {
					if len(svc.Keywords) > 0 || len(svc.Sources) > 0 {
						hasKeywords = true
						break
					}
				}
			}
		}
		if hasKeywords {
			targetNodes := append([]string(nil), input.Scope.NodeIDs...)
			for _, gID := range input.Scope.GroupIDs {
				if g, err := s.Nodes.Group(ctx, gID); err == nil {
					targetNodes = append(targetNodes, g.ServerIDs...)
				}
			}
			if len(targetNodes) == 0 {
				targetNodes = []string{""}
			}
			for _, nID := range targetNodes {
				caps, err := s.NodeCapabilities(ctx, nID)
				if err == nil {
					dummyState := desiredstate.State{Policies: []desiredstate.Policy{{Enabled: true, Match: desiredstate.Match{Keywords: []string{"check"}}}}}
					if err := desiredstate.RequireKeywordSupport(dummyState, caps); err != nil {
						return Record{}, fmt.Errorf("%w: 该服务器的 Agent 不支持关键字规则，请先升级 Agent", desiredstate.ErrKeywordUnsupported)
					}
				}
			}
		}
		if err := s.requireExpandedSupport(ctx, input); err != nil {
			return Record{}, err
		}
	}
	return s.Repository.Save(ctx, ruleID, input)
}

// requireExpandedSupport rejects saving an enabled policy that expands to more
// than MaxMatchers entries when a node it names explicitly (directly or through
// a node group) runs an Agent without ExpandedPolicySupported, and names those
// nodes. A policy for every node checks every non-deleted node the same way;
// it is still checked again when the state is built, because the service sets'
// sources may not be fetched yet at save time.
func (s *Service) requireExpandedSupport(ctx context.Context, input Input) error {
	if !input.Enabled || len(input.Match.ServiceIDs) == 0 {
		return nil
	}
	allNodes := len(input.Scope.NodeIDs)+len(input.Scope.GroupIDs) == 0
	rule := input.Rule("check")
	rule.Enabled = true
	resolved, err := s.resolve(ctx, rule, false)
	if err != nil {
		return err
	}
	count := shared.MatcherCount(resolved)
	if count <= shared.MaxMatchers {
		return nil
	}
	targets := append([]string(nil), input.Scope.NodeIDs...)
	for _, groupID := range input.Scope.GroupIDs {
		if group, err := s.Nodes.Group(ctx, groupID); err == nil {
			targets = append(targets, group.ServerIDs...)
		}
	}
	if allNodes {
		ids, err := s.Nodes.LiveIDs(ctx)
		if err != nil {
			return err
		}
		targets = ids
	}
	state := desiredstate.State{Policies: []desiredstate.Policy{resolved}}
	names := []string{}
	seen := map[string]bool{}
	for _, nodeID := range targets {
		if seen[nodeID] {
			continue
		}
		seen[nodeID] = true
		caps, err := s.NodeCapabilities(ctx, nodeID)
		if err != nil || desiredstate.RequireExpandedPolicySupport(state, caps) == nil {
			continue
		}
		name := nodeID
		if n, err := s.Nodes.Get(ctx, nodeID); err == nil && n.Name != "" {
			name = n.Name
		}
		names = append(names, name)
	}
	if len(names) > 0 {
		return fmt.Errorf("%w: 策略展开规则集后有 %d 条匹配，以下服务器的 Agent 只支持 %d 条，请先升级 Agent：%s", desiredstate.ErrExpandedPolicyUnsupported, count, shared.MaxMatchers, strings.Join(names, "、"))
	}
	return nil
}

// nodeNative reports whether the node's cores read native rule set files
// (v1.5.4 item 5): at least one xray/sing-box core and every one of them
// reports NativeRuleSetsSupported. GOST renders no policies and is ignored.
func (s *Service) nodeNative(ctx context.Context, nodeID string) bool {
	if s.NodeCapabilities == nil {
		return false
	}
	caps, err := s.NodeCapabilities(ctx, nodeID)
	if err != nil {
		return false
	}
	found := false
	for _, core := range caps {
		if core.Name == desiredstate.CoreGost {
			continue
		}
		if !core.NativeRuleSetsSupported {
			return false
		}
		found = true
	}
	return found
}

// resolve expands the service sets a policy names. With native set (the node's
// cores read rule set files), a set that has native sources contributes one
// desiredstate.RuleSet per native source instead of its expanded yaml/text
// sources; its hand-written entries still expand. Without native the native
// sources contribute nothing.
func (s *Service) resolve(ctx context.Context, rule desiredstate.Policy, native bool) (desiredstate.Policy, error) {
	for _, id := range rule.Match.ServiceIDs {
		service, err := s.Repository.Service(ctx, id)
		if err != nil {
			return rule, err
		}
		if !service.Enabled {
			continue
		}
		rule.Match.Domains = append(rule.Match.Domains, service.Domains...)
		rule.Match.Suffixes = append(rule.Match.Suffixes, service.Suffixes...)
		rule.Match.Keywords = append(rule.Match.Keywords, service.Keywords...)
		rule.Match.CIDRs = append(rule.Match.CIDRs, service.CIDRs...)

		if native {
			hasNative := false
			for _, src := range service.Sources {
				if src.Format != "native" {
					continue
				}
				hasNative = true
				rule.Match.RuleSets = append(rule.Match.RuleSets, desiredstate.RuleSet{ID: src.ID, Behavior: src.Behavior, SingBoxURL: src.URL, XrayURL: src.XrayURL, XrayTag: src.XrayTag, RefreshSeconds: src.IntervalSeconds})
			}
			if hasNative {
				continue
			}
		}
		srcEntries, err := s.Repository.SourceEntries(ctx, id)
		if err != nil {
			return rule, err
		}
		rule.Match.Domains = append(rule.Match.Domains, srcEntries.Domains...)
		rule.Match.Suffixes = append(rule.Match.Suffixes, srcEntries.Suffixes...)
		rule.Match.Keywords = append(rule.Match.Keywords, srcEntries.Keywords...)
		rule.Match.CIDRs = append(rule.Match.CIDRs, srcEntries.CIDRs...)
	}
	if len(rule.Match.ServiceIDs) > 0 && shared.MatcherCount(rule) == 0 && len(rule.Match.RuleSets) == 0 {
		rule.Enabled = false
	}
	rule.Match.ServiceIDs = nil
	// The expanded policy may exceed the 2000 entries an Owner can write into
	// one policy; whether the node's Agent accepts that is checked against its
	// capabilities when the state is built (RequireExpandedPolicySupport).
	if count := shared.MatcherCount(rule); count > shared.MaxExpandedMatchers {
		return rule, fmt.Errorf("%w: 策略「%s」展开规则集后有 %d 条匹配，超过上限 %d 条，请减少引用的规则集", shared.ErrMatcherLimit, rule.Name, count, shared.MaxExpandedMatchers)
	}
	return rule, shared.ValidateExpanded(&rule)
}
func (s *Service) ForNode(ctx context.Context, nodeID string) ([]desiredstate.Policy, error) {
	if _, err := s.Nodes.Get(ctx, nodeID); err != nil {
		return nil, err
	}
	groups, err := s.Nodes.GroupIDsForNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	rules, err := s.Repository.Rules(ctx)
	if err != nil {
		return nil, err
	}
	native := s.nodeNative(ctx, nodeID)
	filtered := []desiredstate.Policy{}
	for _, rule := range rules {
		if len(rule.Scope.NodeIDs) > 0 && !contains(rule.Scope.NodeIDs, nodeID) {
			continue
		}
		if len(rule.Scope.GroupIDs) > 0 {
			found := false
			for _, group := range groups {
				if contains(rule.Scope.GroupIDs, group) {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		rule.Scope.NodeIDs = []string{nodeID}
		rule.Scope.GroupIDs = nil
		if rule.Enabled && rule.Action.Type != "Direct" && rule.Action.Type != "Block" {
			owner, err := s.validateTarget(ctx, rule.Action.Type, rule.Action.TargetID, rule.Match.Network)
			if err != nil || owner != nodeID {
				return nil, errors.New("policy target unavailable or transport capability changed; update the policy before publication")
			}
			if err := s.validateActionBackup(ctx, owner, rule.Action.Failover, rule.Action.BackupID, rule.Match.Network); err != nil {
				return nil, err
			}
		}
		rule, err = s.resolve(ctx, rule, native)
		if err != nil {
			return nil, err
		}
		filtered = append(filtered, rule)
	}
	return shared.Sort(filtered), nil
}
func (s *Service) Simulate(ctx context.Context, request shared.Request) (shared.Decision, error) {
	if err := shared.ValidateRequest(&request); err != nil {
		return shared.Decision{}, err
	}
	if n, err := s.Nodes.Get(ctx, request.NodeID); err != nil || n.DeletedAt != "" {
		return shared.Decision{}, errors.New("simulation node not found")
	}
	if request.DeviceID != "" {
		return shared.Decision{}, errors.New("simulation device scope was removed in v1.1; simulate a user")
	}
	if request.UserID != "" {
		u, err := s.Users.Get(ctx, request.UserID)
		if err != nil {
			return shared.Decision{}, errors.New("simulation user not found")
		}
		if !u.Active(request.Time) {
			return shared.Decision{}, errors.New("user is disabled, expired or over quota at simulation time")
		}
	}
	fallback := desiredstate.Action{Type: "Direct", Failover: "Block"}
	if request.InboundID != "" {
		inbound, err := s.Proxies.Inbound(ctx, request.InboundID, false)
		if err != nil || inbound.NodeID != request.NodeID {
			return shared.Decision{}, errors.New("simulation inbound not found on node")
		}
		if inbound.DefaultOutbound == "block" {
			fallback.Type = "Block"
		} else if inbound.DefaultOutbound != "direct" {
			fallback.Type = "Outbound"
			fallback.TargetID = inbound.DefaultOutbound
			if strings.HasPrefix(inbound.DefaultOutbound, "group:") {
				fallback.Type = "OutboundGroup"
				fallback.TargetID = strings.TrimPrefix(inbound.DefaultOutbound, "group:")
			} else if strings.HasPrefix(inbound.DefaultOutbound, "chain:") {
				fallback.Type = "ProxyChain"
				fallback.TargetID = strings.TrimPrefix(inbound.DefaultOutbound, "chain:")
			}
		}
	}
	rules, err := s.ForNode(ctx, request.NodeID)
	if err != nil {
		return shared.Decision{}, err
	}
	decision := shared.Simulate(rules, request, fallback)
	decision.Path = []string{request.NodeID}
	switch decision.Action.Type {
	case "Direct":
		decision.OutboundProtocol = "direct"
		decision.FinalEgressNodeID = request.NodeID
		decision.Path = append(decision.Path, "Direct")
	case "Block":
		decision.OutboundProtocol = "block"
		decision.Path = append(decision.Path, "Block")
	case "Outbound":
		outbound, err := s.Proxies.Outbound(ctx, decision.Action.TargetID, false)
		if err != nil {
			return decision, errors.New("simulation outbound target unavailable")
		}
		decision.OutboundProtocol = outbound.Protocol
		decision.FinalEgressNodeID = request.NodeID
		if outbound.TargetNodeID != "" {
			decision.Path = append(decision.Path, outbound.TargetNodeID)
			decision.FinalEgressNodeID = outbound.TargetNodeID
		} else {
			address := outbound.Address
			if outbound.Protocol == "direct" || outbound.Protocol == "block" {
				address = outbound.Protocol
			}
			decision.Path = append(decision.Path, address)
		}
	case "ProxyChain":
		chain, err := s.Proxies.Chain(ctx, decision.Action.TargetID)
		if err != nil || !chain.Enabled || len(chain.NodeIDs) < 2 || chain.NodeIDs[0] != request.NodeID {
			return decision, errors.New("simulation chain target unavailable")
		}
		decision.Path = append(append([]string{}, chain.NodeIDs...), chain.FinalOutboundID)
		decision.FinalEgressNodeID = chain.NodeIDs[len(chain.NodeIDs)-1]
		if len(chain.Hops) > 0 {
			decision.OutboundProtocol = chain.Hops[0].Protocol
		}
	case "OutboundGroup":
		group, err := s.Proxies.Group(ctx, decision.Action.TargetID)
		if err != nil || !group.Enabled || group.NodeID != request.NodeID {
			return decision, errors.New("simulation outbound group unavailable")
		}
		decision.SelectionStrategy = group.Strategy
		decision.Path = append(decision.Path, "出口组: "+group.Name)
		for _, member := range group.Members {
			outbound, err := s.Proxies.Outbound(ctx, member.OutboundID, false)
			if err != nil {
				return decision, err
			}
			path := []string{request.NodeID}
			if outbound.TargetNodeID != "" {
				path = append(path, outbound.TargetNodeID)
			} else if outbound.Address != "" {
				path = append(path, outbound.Address)
			} else {
				path = append(path, outbound.Protocol)
			}
			decision.CandidatePaths = append(decision.CandidatePaths, path)
		}
	}
	return decision, nil
}

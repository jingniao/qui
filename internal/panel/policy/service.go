package policy

import (
	"context"
	"errors"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
	shared "proxycontrol/pkg/contracts/policy"
	"strings"
	"time"
)

type Nodes interface {
	Get(context.Context, string) (node.Node, error)
	Group(context.Context, string) (node.Group, error)
	GroupIDsForNode(context.Context, string) ([]string, error)
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
	Repository *Repository
	Nodes      Nodes
	Users      Users
	Proxies    Proxies
	Settings   Settings
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
	for _, id := range input.Scope.DeviceIDs {
		if _, err := s.Users.Device(ctx, id); err != nil {
			return Record{}, errors.New("policy device not found")
		}
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
	return s.Repository.Save(ctx, ruleID, input)
}
func (s *Service) resolve(ctx context.Context, rule desiredstate.Policy) (desiredstate.Policy, error) {
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
		rule.Match.CIDRs = append(rule.Match.CIDRs, service.CIDRs...)
	}
	if len(rule.Match.ServiceIDs) > 0 && len(rule.Match.Domains)+len(rule.Match.Suffixes)+len(rule.Match.CIDRs) == 0 {
		rule.Enabled = false
	}
	rule.Match.ServiceIDs = nil
	return rule, shared.Validate(&rule)
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
		rule, err = s.resolve(ctx, rule)
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
		device, err := s.Users.Device(ctx, request.DeviceID)
		if err != nil {
			return shared.Decision{}, errors.New("simulation device not found")
		}
		if request.UserID != "" && request.UserID != device.UserID {
			return shared.Decision{}, errors.New("device does not belong to selected user")
		}
		request.UserID = device.UserID
		if device.Status != "active" {
			return shared.Decision{}, errors.New("device is disabled")
		}
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
func Now() time.Time { return time.Now().UTC() }

package xray

import (
	"errors"
	"proxycontrol/pkg/contracts/desiredstate"
	shared "proxycontrol/pkg/contracts/policy"
	"strings"
	"time"
)

type identityScope struct {
	InboundTags []string
	Users       []string
	Sources     []string
}

func inList(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func scopes(state desiredstate.State, rule desiredstate.Policy) []identityScope {
	if len(rule.Scope.UserIDs) == 0 && len(rule.Scope.DeviceIDs) == 0 {
		return []identityScope{{}}
	}
	credentials := map[string]desiredstate.Credential{}
	for _, credential := range state.Credentials {
		if len(rule.Scope.UserIDs) > 0 && !inList(rule.Scope.UserIDs, credential.UserID) {
			continue
		}
		if len(rule.Scope.DeviceIDs) > 0 && !inList(rule.Scope.DeviceIDs, credential.DeviceID) {
			continue
		}
		credentials[credential.ID] = credential
	}
	result := []identityScope{}
	for _, inbound := range state.Inbounds {
		if !inbound.Enabled {
			continue
		}
		scope := identityScope{InboundTags: []string{inbound.ID}}
		for _, id := range inbound.CredentialIDs {
			credential, exists := credentials[id]
			if !exists {
				continue
			}
			if inbound.Protocol == "wireguard" {
				if inbound.Settings.WireGuard != nil {
					for _, peer := range inbound.Settings.WireGuard.Peers {
						if peer.CredentialID == id {
							scope.Sources = append(scope.Sources, peer.AllowedIPs...)
						}
					}
				}
			} else {
				email := credentialEmail(credential)
				if inbound.Protocol == "http" || inbound.Protocol == "socks5" {
					email = credential.Username
				}
				if email != "" {
					scope.Users = append(scope.Users, email)
				}
			}
		}
		if len(scope.Users) > 0 || len(scope.Sources) > 0 {
			result = append(result, scope)
		}
	}
	return result
}
func compilePolicies(state desiredstate.State, at time.Time) ([]route, error) {
	enabled := map[string]bool{}
	for _, outbound := range state.Outbounds {
		enabled[outbound.ID] = outbound.Enabled
	}
	for _, group := range state.OutboundGroups {
		enabled[group.ID] = true
	}
	for _, chain := range state.ProxyChains {
		if len(chain.NodeIDs) > 0 && chain.NodeIDs[0] == state.NodeID {
			enabled[chain.ID] = true
		}
	}
	rules := []route{}
	for _, policy := range shared.Sort(state.Policies) {
		if err := shared.Validate(&policy); err != nil {
			return nil, err
		}
		if !policy.Enabled {
			continue
		}
		if len(policy.Scope.NodeIDs) > 0 && !inList(policy.Scope.NodeIDs, state.NodeID) {
			continue
		}
		if len(policy.Scope.GroupIDs) > 0 || len(policy.Match.ServiceIDs) > 0 {
			return nil, errors.New("node groups and services must be expanded before compilation")
		}
		target := policy.Action.TargetID
		if policy.Action.Type == "Direct" || policy.Action.Type == "Block" {
			protocol := "direct"
			if policy.Action.Type == "Block" {
				protocol = "block"
			}
			target = ""
			for _, outbound := range state.Outbounds {
				if outbound.Enabled && outbound.Protocol == protocol {
					target = outbound.ID
					break
				}
			}
		} else if policy.Action.Type != "Outbound" && policy.Action.Type != "OutboundGroup" && policy.Action.Type != "ProxyChain" {
			return nil, errors.New("policy action requires configured scheduler target")
		}
		if !enabled[target] {
			return nil, errors.New("policy outbound target unavailable")
		}
		if policy.Action.Type == "Outbound" && policy.Action.Failover != "" && policy.Action.Failover != "Block" {
			return nil, errors.New("policy failover requires configured scheduler target")
		}
		if !shared.TimeActive(policy, at) {
			continue
		}
		domains := []string{}
		for _, domain := range policy.Match.Domains {
			if strings.Contains(domain, "*") {
				domains = append(domains, "regexp:"+shared.Pattern(domain))
			} else {
				domains = append(domains, "full:"+domain)
			}
		}
		for _, suffix := range policy.Match.Suffixes {
			domains = append(domains, "domain:"+suffix)
		}
		for _, scope := range scopes(state, policy) {
			base := route{Type: "field", RuleTag: policy.ID, InboundTag: scope.InboundTags, User: scope.Users, Source: scope.Sources, Network: policy.Match.Network, Port: strings.Join(policy.Match.Ports, ","), OutboundTag: target}
			if base.Network == "" || base.Network == "all" {
				base.Network = "tcp,udp"
			}
			if len(domains) > 0 {
				rule := base
				rule.Domain = domains
				rules = append(rules, rule)
			}
			if len(policy.Match.CIDRs) > 0 {
				rule := base
				rule.IP = policy.Match.CIDRs
				rules = append(rules, rule)
			}
			if len(domains) == 0 && len(policy.Match.CIDRs) == 0 {
				rules = append(rules, base)
			}
		}
	}
	return rules, nil
}

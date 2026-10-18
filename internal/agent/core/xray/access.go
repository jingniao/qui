package xray

import (
	"errors"
	"fmt"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/pkg/contracts/desiredstate"
)

func accessOrigin(original desiredstate.State, outbound desiredstate.Outbound) collector.Exit {
	id := outbound.ID
	for _, rule := range original.Policies {
		if outbound.ID == "_policy_raw_"+rule.ID {
			id = rule.Action.TargetID
			break
		}
	}
	exit := collector.Exit{ID: id, Protocol: outbound.Protocol, LogDomain: true}
	for _, chain := range original.ProxyChains {
		if len(chain.NodeIDs) > 0 && chain.NodeIDs[0] == original.NodeID && len(chain.OutboundIDs) > 0 && chain.OutboundIDs[0] == id {
			exit.ChainID = chain.ID
			break
		}
	}
	return exit
}

// Give each matched rule distinct outbound tags. The core's actual access log
// then identifies the rule even after balancing or selecting a fallback.
func attributeAccessRule(document *configDocument, selected target, ruleID, accountID string, state desiredstate.State) (target, error) {
	logging := true
	for _, rule := range state.Policies {
		if rule.ID == ruleID && rule.Logging != nil {
			logging = rule.Logging.LogDomain
		}
	}
	identity := ruleID
	if accountID != "" {
		identity += "_account_" + accountID
	}
	clone := func(source, tag string) (string, error) {
		if _, exists := document.accessOrigins[tag]; exists {
			return tag, nil
		}
		for _, outbound := range document.Outbounds {
			if outbound.Tag == source {
				copy := outbound
				copy.Tag = tag
				document.Outbounds = append(document.Outbounds, copy)
				exit, known := document.accessOrigins[source]
				if !known {
					return "", errors.New("access outbound origin missing")
				}
				exit.RuleID = ruleID
				exit.LogDomain = logging
				exit.AccountID = accountID
				document.accessOrigins[tag] = exit
				return tag, nil
			}
		}
		return "", errors.New("access outbound missing")
	}
	if selected.Outbound != "" {
		tag, err := clone(selected.Outbound, "_access_rule_"+identity+"_"+selected.Outbound)
		return target{Outbound: tag}, err
	}
	if selected.Balancer != "" {
		tag := "_access_balancer_" + identity + "_" + selected.Balancer
		for _, existing := range document.Routing.Balancers {
			if existing.Tag == tag {
				return target{Balancer: tag}, nil
			}
		}
		for _, balancer := range document.Routing.Balancers {
			if balancer.Tag == selected.Balancer {
				copy := balancer
				copy.Tag = tag
				copy.Selector = []string{}
				for index, source := range balancer.Selector {
					member, err := clone(source, fmt.Sprintf("_access_member_%s_%s_%03d_", identity, selected.Balancer, index))
					if err != nil {
						return target{}, err
					}
					copy.Selector = append(copy.Selector, member)
				}
				fallback, err := clone(balancer.FallbackTag, "_access_fallback_"+identity+"_"+selected.Balancer)
				if err != nil {
					return target{}, err
				}
				copy.FallbackTag = fallback
				document.Routing.Balancers = append(document.Routing.Balancers, copy)
				return target{Balancer: tag}, nil
			}
		}
	}
	return target{}, errors.New("access rule target unavailable")
}

func accessAccountRules(document *configDocument, base route, selected target, state desiredstate.State) ([]route, error) {
	rules := []route{}
	for _, inbound := range state.Inbounds {
		if !inbound.Enabled || len(base.InboundTag) > 0 && !inList(base.InboundTag, inbound.ID) {
			continue
		}
		for _, credential := range state.Credentials {
			if credential.Internal || !inList(inbound.CredentialIDs, credential.ID) {
				continue
			}
			matched := base
			matched.InboundTag = []string{inbound.ID}
			if inbound.Protocol == "wireguard" {
				if inbound.Settings.WireGuard == nil {
					continue
				}
				sources := []string{}
				for _, peer := range inbound.Settings.WireGuard.Peers {
					if peer.CredentialID == credential.ID {
						for _, source := range peer.AllowedIPs {
							if len(base.Source) == 0 || inList(base.Source, source) {
								sources = append(sources, source)
							}
						}
					}
				}
				if len(sources) == 0 {
					continue
				}
				matched.Source = sources
			} else {
				account := credentialEmail(credential)
				if inbound.Protocol == "socks5" || inbound.Protocol == "http" {
					account = credential.Username
				}
				if len(base.User) > 0 && !inList(base.User, account) {
					continue
				}
				matched.User = []string{account}
			}
			selectedAccount, err := attributeAccessRule(document, selected, base.RuleTag, credential.ID, state)
			if err != nil {
				return nil, err
			}
			assignTarget(&matched, selectedAccount)
			rules = append(rules, matched)
		}
	}
	selectedUnknown, err := attributeAccessRule(document, selected, base.RuleTag, "", state)
	if err != nil {
		return nil, err
	}
	assignTarget(&base, selectedUnknown)
	rules = append(rules, base)
	return rules, nil
}

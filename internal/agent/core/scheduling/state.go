package scheduling

import (
	"errors"
	"proxycontrol/pkg/contracts/desiredstate"
)

func State(original desiredstate.State) (desiredstate.State, map[string]string, error) {
	if err := desiredstate.ValidateScheduling(original); err != nil {
		return original, nil, err
	}
	state := original
	state.Outbounds = append([]desiredstate.Outbound(nil), original.Outbounds...)
	state.OutboundGroups = append([]desiredstate.OutboundGroup(nil), original.OutboundGroups...)
	state.Policies = append([]desiredstate.Policy(nil), original.Policies...)
	state.ProxyChains = append([]desiredstate.ProxyChain(nil), original.ProxyChains...)
	aliases := map[string]string{}
	probe := func(id string) (string, int) {
		for _, group := range original.OutboundGroups {
			for _, member := range group.Members {
				if member.OutboundID == id {
					return group.ProbeURL, group.ProbeInterval
				}
			}
		}
		for _, out := range original.Outbounds {
			if out.ID == id {
				url, interval := out.ProbeURL, out.ProbeInterval
				if url == "" {
					url = "https://www.gstatic.com/generate_204"
				}
				if interval == 0 {
					interval = 30
				}
				return url, interval
			}
		}
		return "", 0
	}
	add := func(id, primary, mode, backup string) error {
		url, interval := probe(primary)
		if url == "" {
			return errors.New("failover primary outbound unavailable")
		}
		if mode == "" {
			mode = "Block"
		}
		state.OutboundGroups = append(state.OutboundGroups, desiredstate.OutboundGroup{ID: id, Strategy: "primary", Members: []desiredstate.GroupMember{{OutboundID: primary, Weight: 1}}, ProbeURL: url, ProbeInterval: interval, Failover: mode, BackupTargetID: backup})
		return nil
	}
	for _, out := range original.Outbounds {
		if !out.Enabled || out.FailurePolicy == "" || out.FailurePolicy == "Block" || out.Protocol == "direct" || out.Protocol == "block" {
			continue
		}
		id := "_fail_" + out.ID
		if err := add(id, out.ID, out.FailurePolicy, out.BackupTargetID); err != nil {
			return state, nil, err
		}
		aliases[out.ID] = id
	}
	for _, chain := range original.ProxyChains {
		if len(chain.NodeIDs) == 0 || chain.NodeIDs[0] != state.NodeID || len(chain.OutboundIDs) == 0 || chain.Failover == "" || chain.Failover == "Block" {
			continue
		}
		id := "_chain_fail_" + chain.ID
		if err := add(id, chain.OutboundIDs[0], chain.Failover, chain.BackupTargetID); err != nil {
			return state, nil, err
		}
		aliases[chain.ID] = id
	}
	for index, rule := range original.Policies {
		if !rule.Enabled {
			continue
		}
		mode := rule.Action.Failover
		if mode == "" {
			mode = "Block"
		}
		switch rule.Action.Type {
		case "Outbound":
			if mode == "Block" {
				found := false
				for _, out := range original.Outbounds {
					if out.ID == rule.Action.TargetID && out.Enabled {
						copy := out
						copy.ID = "_policy_raw_" + rule.ID
						copy.FailurePolicy = "Block"
						copy.BackupTargetID = ""
						state.Outbounds = append(state.Outbounds, copy)
						state.Policies[index].Action.TargetID = copy.ID
						found = true
						break
					}
				}
				if !found {
					return state, nil, errors.New("policy outbound unavailable")
				}
			} else {
				id := "_policy_fail_" + rule.ID
				if err := add(id, rule.Action.TargetID, mode, rule.Action.BackupID); err != nil {
					return state, nil, err
				}
				state.Policies[index].Action.Type = "OutboundGroup"
				state.Policies[index].Action.TargetID = id
				state.Policies[index].Action.Failover = "Block"
				state.Policies[index].Action.BackupID = ""
			}
		case "OutboundGroup":
			found := false
			for _, group := range original.OutboundGroups {
				if group.ID == rule.Action.TargetID {
					copy := group
					copy.ID = "_policy_group_" + rule.ID
					copy.Failover = mode
					copy.BackupTargetID = rule.Action.BackupID
					state.OutboundGroups = append(state.OutboundGroups, copy)
					state.Policies[index].Action.TargetID = copy.ID
					found = true
					break
				}
			}
			if !found {
				return state, nil, errors.New("policy group unavailable")
			}
		case "ProxyChain":
			found := false
			for _, chain := range original.ProxyChains {
				if chain.ID == rule.Action.TargetID && len(chain.NodeIDs) > 0 && chain.NodeIDs[0] == state.NodeID && len(chain.OutboundIDs) > 0 {
					copy := chain
					copy.ID = "_policy_chain_" + rule.ID
					copy.Failover = mode
					copy.BackupTargetID = rule.Action.BackupID
					state.ProxyChains = append(state.ProxyChains, copy)
					state.Policies[index].Action.TargetID = copy.ID
					if mode != "Block" {
						id := "_policy_chain_fail_" + rule.ID
						if err := add(id, copy.OutboundIDs[0], mode, copy.BackupTargetID); err != nil {
							return state, nil, err
						}
						aliases[copy.ID] = id
					}
					found = true
					break
				}
			}
			if !found {
				return state, nil, errors.New("policy chain unavailable")
			}
		}
	}
	return state, aliases, nil
}

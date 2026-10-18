package desiredstate

import (
	"errors"
	"fmt"
)

func ValidFailover(mode string) bool {
	switch mode {
	case "", "Block", "Direct", "BackupOutbound", "BackupOutboundGroup", "BackupProxyChain", "BackupChain":
		return true
	}
	return false
}

func BackupKind(mode string) string {
	switch mode {
	case "BackupOutbound":
		return "Outbound"
	case "BackupOutboundGroup":
		return "OutboundGroup"
	case "BackupProxyChain", "BackupChain":
		return "ProxyChain"
	}
	return ""
}

// ValidateScheduling checks references by kind and detects cycles across local
// outbound fallbacks, group members/fallbacks and chain entry paths/fallbacks.
// Other nodes' chain hops are validated by the panel and their own snapshots.
func ValidateScheduling(state State) error {
	kinds := map[string]string{}
	for _, out := range state.Outbounds {
		if out.Enabled {
			kinds[out.ID] = "Outbound"
		}
	}
	for _, group := range state.OutboundGroups {
		kinds[group.ID] = "OutboundGroup"
	}
	for _, chain := range state.ProxyChains {
		if len(chain.NodeIDs) > 0 && chain.NodeIDs[0] == state.NodeID {
			kinds[chain.ID] = "ProxyChain"
		}
	}
	edges := map[string][]string{}
	check := func(id, mode, backup string) error {
		if !ValidFailover(mode) {
			return errors.New("invalid failure policy")
		}
		if kind := BackupKind(mode); kind != "" {
			if backup == "" || kinds[backup] != kind {
				return fmt.Errorf("%s backup target missing or wrong kind", id)
			}
			edges[id] = append(edges[id], backup)
		}
		return nil
	}
	for _, out := range state.Outbounds {
		if out.Enabled {
			if err := check(out.ID, out.FailurePolicy, out.BackupTargetID); err != nil {
				return err
			}
		}
	}
	for _, group := range state.OutboundGroups {
		if err := check(group.ID, group.Failover, group.BackupTargetID); err != nil {
			return err
		}
		for _, member := range group.Members {
			if kinds[member.OutboundID] != "Outbound" {
				return errors.New("group member must be an enabled outbound")
			}
			edges[group.ID] = append(edges[group.ID], member.OutboundID)
		}
	}
	for _, chain := range state.ProxyChains {
		if len(chain.NodeIDs) == 0 || chain.NodeIDs[0] != state.NodeID {
			continue
		}
		if len(chain.OutboundIDs) != len(chain.NodeIDs)-1 || kinds[chain.OutboundIDs[0]] != "Outbound" {
			return errors.New("chain entry outbound unavailable")
		}
		if err := check(chain.ID, chain.Failover, chain.BackupTargetID); err != nil {
			return err
		}
		edges[chain.ID] = append(edges[chain.ID], chain.OutboundIDs[0])
	}
	for _, rule := range state.Policies {
		if !rule.Enabled {
			continue
		}
		if kind := BackupKind(rule.Action.Failover); kind != "" && kinds[rule.Action.BackupID] != kind {
			return errors.New("policy backup target missing or wrong kind")
		}
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return errors.New("scheduling target reference cycle")
		}
		if done[id] {
			return nil
		}
		visiting[id] = true
		for _, next := range edges[id] {
			if err := visit(next); err != nil {
				return err
			}
		}
		delete(visiting, id)
		done[id] = true
		return nil
	}
	for id := range kinds {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

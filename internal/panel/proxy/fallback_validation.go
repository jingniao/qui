package proxy

import (
	"context"
	"errors"
	"proxycontrol/pkg/contracts/desiredstate"
)

func (s *Service) checkSchedulingChange(ctx context.Context, nodeID, replaceID string, replacement any) error {
	state := desiredstate.State{NodeID: nodeID}
	outs, err := snapshotList(func(page, size int) ([]Outbound, int, error) {
		return s.Repository.Outbounds(ctx, nodeID, page, size, false)
	})
	if err != nil {
		return err
	}
	groups, err := snapshotList(func(page, size int) ([]Group, int, error) { return s.Repository.Groups(ctx, nodeID, page, size) })
	if err != nil {
		return err
	}
	chains, err := snapshotList(func(page, size int) ([]Chain, int, error) { return s.Repository.Chains(ctx, nodeID, page, size) })
	if err != nil {
		return err
	}
	for _, out := range outs {
		if out.ID != replaceID && out.Enabled {
			state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: out.ID, Enabled: true, FailurePolicy: out.FailurePolicy, BackupTargetID: out.BackupOutboundID})
		}
	}
	for _, group := range groups {
		if group.ID != replaceID && group.Enabled {
			state.OutboundGroups = append(state.OutboundGroups, group.Desired())
		}
	}
	for _, chain := range chains {
		if chain.ID != replaceID && chain.Enabled && chain.NodeIDs[0] == nodeID {
			state.ProxyChains = append(state.ProxyChains, chain.Desired())
		}
	}
	switch value := replacement.(type) {
	case OutboundInput:
		if value.Enabled {
			state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: replaceID, Enabled: true, FailurePolicy: value.FailurePolicy, BackupTargetID: value.BackupOutboundID})
		}
	case Group:
		if value.Enabled {
			state.OutboundGroups = append(state.OutboundGroups, value.Desired())
		}
	case Chain:
		if value.Enabled {
			state.ProxyChains = append(state.ProxyChains, value.Desired())
		}
	}
	return desiredstate.ValidateScheduling(state)
}

func (s *Service) validateBackup(ctx context.Context, owner, mode, targetID string, tcp, udp bool) error {
	kind := desiredstate.BackupKind(mode)
	if !desiredstate.ValidFailover(mode) {
		return errors.New("invalid failure policy")
	}
	if kind == "" {
		return nil
	}
	seen := map[string]bool{}
	var validate func(string, string) error
	validate = func(kind, id string) error {
		key := kind + ":" + id
		if seen[key] {
			return errors.New("backup target cycle")
		}
		seen[key] = true
		defer delete(seen, key)
		var nextMode, nextID string
		switch kind {
		case "Outbound":
			out, err := s.Repository.Outbound(ctx, id, false)
			if err != nil || !out.Enabled || out.NodeID != owner || tcp && !out.SupportTCP || udp && !out.SupportUDP {
				return errors.New("backup outbound unavailable or TCP/UDP capability mismatch")
			}
			nextMode, nextID = out.FailurePolicy, out.BackupOutboundID
		case "OutboundGroup":
			group, err := s.Repository.Group(ctx, id)
			if err != nil || !group.Enabled || group.NodeID != owner {
				return errors.New("backup group must be enabled on the same entry node")
			}
			for _, member := range group.Members {
				if err := validate("Outbound", member.OutboundID); err != nil {
					return err
				}
			}
			nextMode, nextID = group.FailurePolicy, group.BackupOutboundID
		case "ProxyChain":
			chain, err := s.Repository.Chain(ctx, id)
			if err != nil || !chain.Enabled || len(chain.NodeIDs) < 2 || chain.NodeIDs[0] != owner || tcp && !chain.SupportTCP || udp && !chain.SupportUDP {
				return errors.New("backup chain unavailable or TCP/UDP capability mismatch")
			}
			for index, hop := range chain.Hops {
				out, err := s.Repository.Outbound(ctx, hop.OutboundID, false)
				if err != nil || !out.Enabled || out.NodeID != chain.NodeIDs[index] || out.TargetNodeID != chain.NodeIDs[index+1] || tcp && !out.SupportTCP || udp && !out.SupportUDP {
					return errors.New("backup chain hop unavailable")
				}
			}
			nextMode, nextID = chain.FailurePolicy, chain.BackupTargetID
		default:
			return errors.New("unknown backup target type")
		}
		if next := desiredstate.BackupKind(nextMode); next != "" {
			return validate(next, nextID)
		}
		return nil
	}
	return validate(kind, targetID)
}

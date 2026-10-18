package policy

import (
	"context"
	"errors"
	"proxycontrol/pkg/contracts/desiredstate"
)

func (s *Service) validateTarget(ctx context.Context, kind, targetID, network string) (string, error) {
	tcp, udp := network != "udp", network != "tcp"
	switch kind {
	case "Outbound":
		outbound, err := s.Proxies.Outbound(ctx, targetID, false)
		if err != nil || !outbound.Enabled || tcp && !outbound.SupportTCP || udp && !outbound.SupportUDP {
			return "", errors.New("policy outbound unavailable or TCP/UDP capability mismatch")
		}
		return outbound.NodeID, nil
	case "OutboundGroup":
		group, err := s.Proxies.Group(ctx, targetID)
		if err != nil || !group.Enabled || len(group.Members) == 0 {
			return "", errors.New("policy outbound group unavailable")
		}
		for _, member := range group.Members {
			owner, err := s.validateTarget(ctx, "Outbound", member.OutboundID, network)
			if err != nil || owner != group.NodeID {
				return "", errors.New("group policy member unavailable or TCP/UDP capability mismatch")
			}
		}
		return group.NodeID, nil
	case "ProxyChain":
		chain, err := s.Proxies.Chain(ctx, targetID)
		if err != nil || !chain.Enabled || len(chain.NodeIDs) < 2 || len(chain.Hops) != len(chain.NodeIDs)-1 || tcp && !chain.SupportTCP || udp && !chain.SupportUDP {
			return "", errors.New("policy chain unavailable or TCP/UDP capability mismatch")
		}
		for index, hop := range chain.Hops {
			owner, err := s.validateTarget(ctx, "Outbound", hop.OutboundID, network)
			if err != nil || owner != chain.NodeIDs[index] {
				return "", errors.New("policy chain hop unavailable or TCP/UDP capability mismatch")
			}
		}
		if chain.FinalOutboundID != "direct" && chain.FinalOutboundID != "block" {
			owner, err := s.validateTarget(ctx, "Outbound", chain.FinalOutboundID, network)
			if err != nil || owner != chain.NodeIDs[len(chain.NodeIDs)-1] {
				return "", errors.New("policy chain final outbound unavailable")
			}
		}
		return chain.NodeIDs[0], nil
	}
	return "", errors.New("invalid policy target type")
}

func (s *Service) validateActionBackup(ctx context.Context, owner, mode, targetID, network string) error {
	seen := map[string]bool{}
	for kind := desiredstate.BackupKind(mode); kind != ""; kind = desiredstate.BackupKind(mode) {
		key := kind + ":" + targetID
		if seen[key] {
			return errors.New("policy backup cycle")
		}
		seen[key] = true
		actual, err := s.validateTarget(ctx, kind, targetID, network)
		if err != nil || actual != owner {
			return errors.New("policy backup must match entry node and TCP/UDP capability")
		}
		switch kind {
		case "Outbound":
			value, err := s.Proxies.Outbound(ctx, targetID, false)
			if err != nil {
				return err
			}
			mode, targetID = value.FailurePolicy, value.BackupOutboundID
		case "OutboundGroup":
			value, err := s.Proxies.Group(ctx, targetID)
			if err != nil {
				return err
			}
			mode, targetID = value.FailurePolicy, value.BackupOutboundID
		case "ProxyChain":
			value, err := s.Proxies.Chain(ctx, targetID)
			if err != nil {
				return err
			}
			mode, targetID = value.FailurePolicy, value.BackupTargetID
		}
	}
	return nil
}

func (s *Service) UsesProxyTarget(ctx context.Context, targetID string) (bool, error) {
	return s.Repository.UsesProxyTarget(ctx, targetID)
}

func (r *Repository) UsesProxyTarget(ctx context.Context, targetID string) (bool, error) {
	var count int
	err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM policies WHERE json_extract(action_json,'$.target_id')=? OR json_extract(action_json,'$.backup_id')=?", targetID, targetID).Scan(&count)
	return count > 0, err
}

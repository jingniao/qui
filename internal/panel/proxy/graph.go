package proxy

import (
	"context"
	"errors"
)

func (s *Service) checkRelayCycle(ctx context.Context, source, target, replaceID string) error {
	if target == "" {
		return nil
	}
	outbounds, err := s.relayGraphOutbounds(ctx)
	if err != nil {
		return err
	}
	return checkRelayCycle(outbounds, source, target, replaceID)
}

func (s *Service) relayGraphOutbounds(ctx context.Context) ([]Outbound, error) {
	outbounds, total, err := s.Repository.Outbounds(ctx, "", 1, 10000, false)
	if err != nil {
		return nil, err
	}
	if total > 10000 {
		return nil, errors.New("relay graph exceeds supported configuration size")
	}
	return outbounds, nil
}

// checkRelayCycle is shared by an individual outbound save and the canvas's
// read-only simulation of those saves.
func checkRelayCycle(outbounds []Outbound, source, target, replaceID string) error {
	if target == "" {
		return nil
	}
	edges := map[string][]string{}
	for _, outbound := range outbounds {
		if !outbound.Enabled || outbound.TargetNodeID == "" || outbound.ID == replaceID {
			continue
		}
		edges[outbound.NodeID] = append(edges[outbound.NodeID], outbound.TargetNodeID)
	}
	seen := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if id == source {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, next := range edges[id] {
			if visit(next) {
				return true
			}
		}
		return false
	}
	if visit(target) {
		return errors.New("relay outbound would create a routing loop")
	}
	return nil
}

package proxy

import (
	"context"
	"errors"
	"fmt"
)

// canvasCheckRelayCycles simulates canvasEnsure's outbound saves in order. The
// graph includes enabled relays outside the drawing, including stale owned
// relays: canvasEnsure saves the new objects before canvasRemoveStale deletes
// the old ones. Omitting those edges would approve a plan SaveOutbound rejects.
// Reused objects replace only their own ID; names do not remove other edges.
func (s *Service) canvasCheckRelayCycles(ctx context.Context, plan *canvasPlan) error {
	if len(plan.edges) == 0 {
		return nil
	}
	outbounds, err := s.relayGraphOutbounds(ctx)
	if err != nil {
		return err
	}
	for index, edge := range plan.edges {
		existing, err := s.Repository.canvasOutboundNamed(ctx, edge.sourceNode, edge.outboundName)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := checkRelayCycle(outbounds, edge.sourceNode, edge.targetNode, existing.ID); err != nil {
			return fmt.Errorf("连接 %q：%w", edge.connectionID, err)
		}
		candidate := Outbound{ID: existing.ID, NodeID: edge.sourceNode, TargetNodeID: edge.targetNode, Enabled: true}
		if existing.ID == "" {
			// These IDs exist only in memory and cannot collide with stored IDs.
			candidate.ID = fmt.Sprintf("\x00canvas:%d", index)
			outbounds = append(outbounds, candidate)
			continue
		}
		for i := range outbounds {
			if outbounds[i].ID == existing.ID {
				outbounds[i] = candidate
				break
			}
		}
	}
	return nil
}

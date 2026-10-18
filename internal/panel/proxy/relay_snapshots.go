package proxy

import (
	"context"
	"errors"
	"sort"

	"proxycontrol/pkg/contracts/desiredstate"
)

// RelaySnapshots includes the enabled relay component around a changed node,
// including its upstream callers and their downstream dependencies.
func (s *Service) RelaySnapshots(ctx context.Context, root string, users AccessReader) ([]desiredstate.State, error) {
	if root == "" || users == nil {
		return nil, errors.New("node and access reader required")
	}
	rows, err := s.Repository.Database.QueryContext(ctx, `SELECT DISTINCT o.node_id,o.target_node_id FROM outbounds o JOIN nodes n ON n.id=o.node_id WHERE o.enabled=1 AND n.enabled=1 AND o.target_node_id IS NOT NULL AND o.target_node_id<>'' ORDER BY o.node_id,o.target_node_id LIMIT 4097`)
	if err != nil {
		return nil, err
	}
	graph, neighbors := map[string]map[string]bool{}, map[string][]string{}
	edges := 0
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			rows.Close()
			return nil, err
		}
		edges++
		if graph[from] == nil {
			graph[from] = map[string]bool{}
		}
		graph[from][to] = true
		neighbors[from] = append(neighbors[from], to)
		neighbors[to] = append(neighbors[to], from)
	}
	readErr := rows.Err()
	if err := errors.Join(readErr, rows.Close()); err != nil {
		return nil, err
	}
	if edges > 4096 {
		return nil, errors.New("relay dependency graph exceeds bounds")
	}
	selected := map[string]bool{root: true}
	queue := []string{root}
	for index := 0; index < len(queue); index++ {
		for _, neighbor := range neighbors[queue[index]] {
			if selected[neighbor] {
				continue
			}
			if len(selected) >= 64 {
				return nil, errors.New("associated deployment exceeds 64 nodes")
			}
			selected[neighbor] = true
			queue = append(queue, neighbor)
		}
	}
	if len(selected) < 2 {
		return nil, errors.New("node has no enabled relay relationships; create a single-node revision")
	}
	sort.Strings(queue)
	states := map[string]desiredstate.State{}
	for _, nodeID := range queue {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		state, err := s.Snapshot(ctx, nodeID, users)
		if err != nil {
			return nil, err
		}
		n, err := s.Nodes.Get(ctx, nodeID)
		if err != nil || !n.Enabled {
			return nil, errors.New("associated node unavailable")
		}
		for _, inbound := range state.Inbounds {
			if !inbound.Enabled {
				continue
			}
			supported := false
			for _, ability := range n.Capabilities.Protocols {
				if ability.Protocol == inbound.Protocol && ability.Inbound {
					supported = true
				}
			}
			if !supported {
				return nil, errors.New("associated node does not support an enabled inbound protocol")
			}
		}
		actual := map[string]bool{}
		for _, outbound := range state.Outbounds {
			if !outbound.Enabled {
				continue
			}
			if outbound.Protocol != "direct" && outbound.Protocol != "block" {
				supported := false
				for _, ability := range n.Capabilities.Protocols {
					if ability.Protocol == outbound.Protocol && ability.Outbound && (!outbound.SupportTCP || ability.Tcp) && (!outbound.SupportUDP || ability.Udp) {
						supported = true
					}
				}
				if !supported {
					return nil, errors.New("associated node does not support an enabled outbound capability")
				}
			}
			if outbound.TargetNodeID != "" {
				actual[outbound.TargetNodeID] = true
			}
		}
		if len(actual) != len(graph[nodeID]) {
			return nil, errors.New("relay graph changed while creating snapshots; retry")
		}
		for target := range actual {
			if !selected[target] || !graph[nodeID][target] {
				return nil, errors.New("relay graph changed while creating snapshots; retry")
			}
		}
		states[nodeID] = state
	}
	result := make([]desiredstate.State, 0, len(states))
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(nodeID string) error {
		if visiting[nodeID] {
			return errors.New("associated deployment dependency cycle")
		}
		if done[nodeID] {
			return nil
		}
		visiting[nodeID] = true
		targets := make([]string, 0, len(graph[nodeID]))
		for target := range graph[nodeID] {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			if err := visit(target); err != nil {
				return err
			}
		}
		delete(visiting, nodeID)
		done[nodeID] = true
		result = append(result, states[nodeID])
		return nil
	}
	for _, nodeID := range queue {
		if err := visit(nodeID); err != nil {
			return nil, err
		}
	}
	return result, nil
}

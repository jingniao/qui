package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"proxycontrol/pkg/contracts/desiredstate"
)

// Allocate only implicit relay listeners. Ports are reserved by number on a node:
// the Agent rejects a second enabled listener on one number whatever its
// transport, so allocation skips every port any inbound of the node already uses
// (enabled or not) plus this plan's own listeners. Existing generated names
// remain reusable so repeated plan/publish calls choose the same port.
func (s *Service) canvasAllocatePorts(ctx context.Context, plan *canvasPlan, names map[string]string) error {
	owned, err := s.canvasOwnedInbounds(ctx, plan)
	if err != nil {
		return err
	}
	ownedIDs := map[string]bool{}
	for _, i := range owned {
		ownedIDs[i.ID] = true
	}
	for _, edge := range plan.edges {
		if edge.autoPort && edge.tunnelID != "" {
			t, err := s.Repository.canvasTunnel(ctx, edge.tunnelID)
			if err != nil {
				return err
			}
			if t.allocatedPort != nil {
				edge.port = *t.allocatedPort
				edge.autoPort = false
			}
		}
		for _, i := range owned {
			owner, e := s.Repository.objectOwner(ctx, "inbound", i.ID)
			if e != nil {
				return e
			}
			if owner == edge.tunnelID && i.NodeID == edge.targetNode && (i.Protocol == edge.protocol || edge.protocol == "trojan" && i.InternalRelay) && edge.autoPort {
				edge.port = i.Port
				edge.autoPort = false
				break
			}
		}
		if !edge.autoPort {
			continue
		}
		exclude := []PortOwner{}
		for _, i := range owned {
			name, _ := canvasEdgeNames(edge.prefix, names[edge.sourceNode], names[edge.targetNode], edge.protocol, i.Port)
			if ownedIDs[i.ID] && i.Name == name {
				exclude = append(exclude, PortOwner{Kind: "inbound", ID: i.ID})
			}
		}
		occupied, err := s.Repository.portOccupants(ctx, s.Repository.Database, edge.targetNode, exclude)
		if err != nil {
			return err
		}
		taken := map[int]bool{}
		canvasEntryPorts(plan, edge.targetNode, taken)
		for _, other := range plan.edges {
			if other != edge && other.targetNode == edge.targetNode && (!other.autoPort || other.portAllocated) {
				taken[other.port] = true
			}
		}
		for _, f := range plan.forwards {
			if f.NodeID == edge.targetNode {
				for _, occupied := range desiredstate.ClaimedPorts(f.Port, f.Settings) {
					taken[occupied] = true
				}
			}
		}
		ports, err := allocatePorts(ctx, s.Repository.Database, edge.targetNode, 1, occupied, taken)
		if err != nil {
			var start *int
			e := s.Repository.Database.QueryRowContext(ctx, "SELECT port_range_start FROM node_meta WHERE node_id=?", edge.targetNode).Scan(&start)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if (e == nil && start != nil) || len(occupied[edge.port]) > 0 || taken[edge.port] {
				return errors.New("服务器端口范围内没有可用的中转监听端口")
			}
			ports = []int{edge.port} // Legacy canvas default: TLS 8443 / WireGuard 51820.
		}
		edge.port, edge.portAllocated = ports[0], true
	}
	for _, relay := range plan.relays {
		if err := s.allocateRelayPort(ctx, plan, relay); err != nil {
			return err
		}
	}
	return nil
}

// Allocate user entries after shared dependencies have supplied all listeners.
func (s *Service) canvasAllocateEntryPorts(ctx context.Context, plan *canvasPlan) error {
	for index := range plan.entries {
		entry := &plan.entries[index]
		if entry.binding == nil || entry.binding.Mode != "create" || entry.existing.ID != "" {
			continue
		}
		e := entry.preview
		mode := "enabled"
		if entry.binding.PortMode == "auto" {
			mode = "all"
		}
		occupied, err := s.Repository.portOccupants(ctx, s.Repository.Database, e.NodeID, nil, mode)
		if err != nil {
			return err
		}
		taken := map[int]bool{}
		canvasEntryPorts(plan, e.NodeID, taken)
		for _, edge := range plan.edges {
			if edge.targetNode == e.NodeID {
				taken[edge.port] = true
			}
		}
		for _, f := range plan.forwards {
			if f.NodeID == e.NodeID {
				for _, port := range desiredstate.ClaimedPorts(f.Port, f.Settings) {
					taken[port] = true
				}
			}
		}
		for _, relay := range plan.relays {
			if relay.exitNode == e.NodeID {
				taken[relay.port] = true
			}
		}
		if entry.binding.PortMode == "auto" {
			ports, err := allocatePorts(ctx, s.Repository.Database, e.NodeID, 1, occupied, taken)
			if err != nil {
				return err
			}
			e.Port = ports[0]
		} else if len(occupied[e.Port]) > 0 {
			return fmt.Errorf("入口端口 %d 已被 %s 占用", e.Port, occupied[e.Port][0].Name)
		}
		// The common listener check diagnoses manual collisions within the plan.
	}
	return nil
}

func canvasEntryPorts(plan *canvasPlan, nodeID string, taken map[int]bool) {
	for _, entry := range plan.entries {
		if entry.binding != nil && entry.binding.Mode == "create" && entry.preview.NodeID == nodeID && entry.preview.Port != 0 {
			taken[entry.preview.Port] = true
		}
	}
}

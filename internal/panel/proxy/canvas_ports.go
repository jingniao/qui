package proxy

import (
	"context"
	"database/sql"
	"errors"
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
			if owner == edge.tunnelID && i.NodeID == edge.targetNode && i.Protocol == edge.protocol && edge.autoPort {
				edge.port = i.Port
				edge.autoPort = false
				break
			}
		}
		if !edge.autoPort {
			continue
		}
		var start, end sql.NullInt64
		err := s.Repository.Database.QueryRowContext(ctx, "SELECT port_range_start,port_range_end FROM node_meta WHERE node_id=?", edge.targetNode).Scan(&start, &end)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if !start.Valid {
			continue
		}
		inbounds, err := snapshotList(func(page, size int) ([]Inbound, int, error) {
			return s.Repository.Inbounds(ctx, edge.targetNode, page, size, true)
		})
		if err != nil {
			return err
		}
		found := false
		for port := int(start.Int64); port <= int(end.Int64); port++ {
			name, _ := canvasEdgeNames(edge.prefix, names[edge.sourceNode], names[edge.targetNode], edge.protocol, port)
			busy := false
			for _, i := range inbounds {
				// A number used by either transport is unavailable; the edge's own
				// generated listener stays reusable by name.
				if i.Port == port && !(ownedIDs[i.ID] && i.Name == name) {
					busy = true
					break
				}
			}
			for _, other := range plan.edges {
				if other != edge && other.targetNode == edge.targetNode && other.port == port && (!other.autoPort || other.portAllocated) {
					busy = true
				}
			}
			for _, f := range plan.forwards {
				if f.NodeID == edge.targetNode && f.Port == port {
					busy = true
				}
			}
			if !busy {
				edge.port = port
				edge.portAllocated = true
				found = true
				break
			}
		}
		if !found {
			return errors.New("服务器端口范围内没有可用的中转监听端口")
		}
	}
	return nil
}

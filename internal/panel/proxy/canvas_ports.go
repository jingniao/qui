package proxy

import (
	"context"
	"database/sql"
	"errors"
)

// Allocate only implicit relay listeners. Both disabled listeners and planned
// listeners reserve their transports. Existing generated names remain reusable
// so repeated plan/publish calls choose the same port.
func (s *Service) canvasAllocatePorts(ctx context.Context, plan *canvasPlan, names map[string]string) error {
	owned, err := s.Repository.canvasInboundsByPrefix(ctx, plan.prefix)
	if err != nil {
		return err
	}
	ownedIDs := map[string]bool{}
	for _, i := range owned {
		ownedIDs[i.ID] = true
	}
	for _, edge := range plan.edges {
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
			name, _ := canvasEdgeNames(plan.prefix, names[edge.sourceNode], names[edge.targetNode], edge.protocol, port)
			networks := listenNetworks(edge.protocol, "")
			busy := false
			for _, i := range inbounds {
				if i.Port == port && networksOverlap(networks, inboundNetworks(i)) && !(ownedIDs[i.ID] && i.Name == name) {
					busy = true
					break
				}
			}
			for _, other := range plan.edges {
				if other != edge && other.targetNode == edge.targetNode && other.port == port && (!other.autoPort || other.portAllocated) && networksOverlap(networks, listenNetworks(other.protocol, "")) {
					busy = true
				}
			}
			for _, f := range plan.forwards {
				if f.NodeID == edge.targetNode && f.Port == port && networksOverlap(networks, listenNetworks(f.Protocol, f.Settings.Forward.Network)) {
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

package proxy

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Keep the connected relay component around a shared hop. Invalid unrelated
// draft components must not block or enter another drawing's publication.
func (s *Service) canvasDependencyLayout(ctx context.Context, layout CanvasLayout, wanted map[string]bool) (CanvasLayout, error) {
	refs := map[string]string{}
	for _, i := range layout.Instances {
		refs[i.InstanceID] = i.Ref
	}
	chosen := map[string]bool{}
	nodes := map[string]bool{}
	for _, c := range layout.Connections {
		if wanted[c.TunnelID] {
			chosen[c.ID] = true
			if c.Type != "port_forward" {
				nodes[refs[c.Source]] = true
				nodes[refs[c.Target]] = true
			}
		}
	}
	for {
		changed := false
		for _, c := range layout.Connections {
			if chosen[c.ID] || c.Type == "port_forward" {
				continue
			}
			if nodes[refs[c.Source]] || nodes[refs[c.Target]] {
				chosen[c.ID] = true
				nodes[refs[c.Source]] = true
				nodes[refs[c.Target]] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	result := CanvasLayout{Instances: []CanvasInstance{}, Connections: []CanvasConnection{}}
	instances := map[string]bool{}
	for _, c := range layout.Connections {
		if chosen[c.ID] {
			result.Connections = append(result.Connections, c)
			instances[c.Source] = true
			instances[c.Target] = true
		}
	}
	for _, i := range layout.Instances {
		include := instances[i.InstanceID]
		if strings.HasPrefix(i.Ref, "inbound:") {
			var nodeID string
			if err := s.Repository.Database.QueryRowContext(ctx, "SELECT node_id FROM inbounds WHERE id=?", strings.TrimPrefix(i.Ref, "inbound:")).Scan(&nodeID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return result, err
			}
			include = nodes["server:"+nodeID]
		}
		if include {
			result.Instances = append(result.Instances, i)
		}
	}
	return result, nil
}

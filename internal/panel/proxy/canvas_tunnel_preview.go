package proxy

import (
	"context"
	"errors"
)

func (r *Repository) attachInlineTunnelIDs(ctx context.Context, schemeID string, layout *CanvasLayout) error {
	*layout = layout.normalized()
	layout.Connections = append([]CanvasConnection{}, layout.Connections...)
	if schemeID == "" {
		return nil
	}
	var raw string
	if err := r.Database.QueryRowContext(ctx, "SELECT layout_json FROM canvas_schemes WHERE id=?", schemeID).Scan(&raw); err != nil {
		return err
	}
	old, err := decodeCanvasLayout([]byte(raw))
	if err != nil {
		return err
	}
	for n, c := range layout.Connections {
		if c.TunnelID != "" {
			continue
		}
		for _, previous := range old.Connections {
			if previous.ID == c.ID && previous.TunnelID != "" {
				t, err := r.canvasTunnel(ctx, previous.TunnelID)
				if err != nil {
					return err
				}
				if t.inlineScheme == schemeID {
					layout.Connections[n].TunnelID = t.ID
				}
				break
			}
		}
	}
	return nil
}

// Use object identity after explicit adoption. Names may have been changed by
// the Owner; only compatible slots in this tunnel can satisfy a desired object.
func (s *Service) canvasReuseOwnedNames(ctx context.Context, p *canvasPlan) error {
	for _, e := range p.edges {
		if e.tunnelID == "" {
			continue
		}
		objects, err := s.Repository.tunnelObjects(ctx, e.tunnelID)
		if err != nil {
			return err
		}
		for _, o := range objects {
			switch o.Kind {
			case "inbound":
				i, err := s.Repository.Inbound(ctx, o.ID, true)
				if err != nil {
					return err
				}
				if i.NodeID == e.targetNode && i.Protocol == e.protocol && i.Port == e.port {
					delete(p.inbounds, e.inboundName)
					delete(p.owners, e.inboundName)
					e.inboundName = i.Name
					p.inbounds[i.Name] = true
					p.owners[i.Name] = e.tunnelID
				}
			case "outbound":
				b, err := s.Repository.Outbound(ctx, o.ID, true)
				if err != nil {
					return err
				}
				if b.NodeID != e.sourceNode || b.Protocol != e.protocol {
					continue
				}
				i, err := s.Repository.Inbound(ctx, b.TargetInboundID, true)
				if err != nil {
					return err
				}
				if i.NodeID == e.targetNode && i.Protocol == e.protocol && i.Port == e.port {
					delete(p.outbounds, e.outboundName)
					delete(p.owners, e.outboundName)
					e.outboundName = b.Name
					p.outbounds[b.Name] = true
					p.owners[b.Name] = e.tunnelID
				}
			}
		}
	}
	for n := range p.forwards {
		f := &p.forwards[n]
		owner := p.owners[f.Name]
		if owner == "" {
			continue
		}
		objects, err := s.Repository.tunnelObjects(ctx, owner)
		if err != nil {
			return err
		}
		for _, o := range objects {
			if o.Kind != "inbound" {
				continue
			}
			i, err := s.Repository.Inbound(ctx, o.ID, true)
			if err != nil {
				return err
			}
			if i.NodeID == f.NodeID && i.Protocol == "forward" && i.Port == f.Port {
				delete(p.inbounds, f.Name)
				delete(p.owners, f.Name)
				f.Name = i.Name
				p.inbounds[i.Name] = true
				p.owners[i.Name] = owner
				break
			}
		}
	}
	return nil
}

func (s *Service) canvasNamedObjectConflicts(ctx context.Context, p *canvasPlan) ([]CanvasPlanConflict, error) {
	result := []CanvasPlanConflict{}
	for name, owner := range p.owners {
		kind, table := "inbound", "inbounds"
		if p.outbounds[name] {
			kind, table = "outbound", "outbounds"
		}
		if p.chainName[name] {
			kind, table = "chain", "proxy_chains"
		}
		rows, err := s.Repository.Database.QueryContext(ctx, "SELECT id FROM "+table+" WHERE name=?", name)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		for _, id := range ids {
			actual, err := s.Repository.objectOwner(ctx, kind, id)
			if err != nil {
				return nil, err
			}
			if actual == owner && owner != "" {
				continue
			}
			reason := "object_name"
			if actual == "" && legacyCanvasName(kind, name) {
				reason = "legacy_object"
			}
			result = append(result, CanvasPlanConflict{Reason: reason, ObjectID: id, With: name})
		}
	}
	return result, nil
}

func (s *Service) checkCanvasNamedObjects(ctx context.Context, p *canvasPlan) error {
	conflicts, err := s.canvasNamedObjectConflicts(ctx, p)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return errors.New(conflicts[0].Reason + ": " + conflicts[0].ObjectID)
	}
	return nil
}

func (s *Service) canvasOwnedChainName(ctx context.Context, path []*canvasEdge, fallback string) (string, error) {
	if len(path) == 0 || path[0].tunnelID == "" {
		return fallback, nil
	}
	objects, err := s.Repository.tunnelObjects(ctx, path[0].tunnelID)
	if err != nil {
		return "", err
	}
	matched := ""
	for _, object := range objects {
		if object.Kind != "chain" {
			continue
		}
		chain, err := s.Repository.Chain(ctx, object.ID)
		if err != nil {
			return "", err
		}
		if len(chain.OutboundIDs) != len(path) {
			continue
		}
		same := true
		for n, outboundID := range chain.OutboundIDs {
			owner, err := s.Repository.objectOwner(ctx, "outbound", outboundID)
			if err != nil {
				return "", err
			}
			if owner != path[n].tunnelID {
				same = false
				break
			}
		}
		if same {
			if matched != "" {
				return "", errors.New("multiple owned chains implement the same tunnel path")
			}
			matched = chain.Name
		}
	}
	if matched != "" {
		return matched, nil
	}
	return fallback, nil
}

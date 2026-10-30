package proxy

import (
	"context"
	"errors"
	"strings"
)

type CanvasTunnelImpact struct {
	TunnelID        string            `json:"tunnelId"`
	AffectedSchemes []CanvasSchemeRef `json:"affectedSchemes"`
}

// Entry routing is an exclusive intent across server drawings. Comparing paths
// of tunnel identities avoids treating two drawings of the same path as a
// disagreement, even when their instance and connection IDs differ.
func (s *Service) canvasEntryRoutingConflict(ctx context.Context, scheme CanvasScheme, plan *canvasPlan) error {
	routes := func(layout CanvasLayout) map[string]string {
		refs := map[string]string{}
		for _, i := range layout.Instances {
			refs[i.InstanceID] = i.Ref
		}
		outgoing := map[string][]CanvasConnection{}
		for _, c := range layout.Connections {
			if c.Type != "port_forward" {
				outgoing[refs[c.Source]] = append(outgoing[refs[c.Source]], c)
			}
		}
		result := map[string]string{}
		for _, entry := range layout.Instances {
			if !strings.HasPrefix(entry.Ref, "inbound:") {
				continue
			}
			var node string
			_ = s.Repository.Database.QueryRowContext(ctx, "SELECT node_id FROM inbounds WHERE id=?", strings.TrimPrefix(entry.Ref, "inbound:")).Scan(&node)
			ref := "server:" + node
			path := []string{}
			seen := map[string]bool{}
			for !seen[ref] {
				seen[ref] = true
				edges := outgoing[ref]
				if len(edges) != 1 {
					break
				}
				c := edges[0]
				key := c.TunnelID
				if key == "" {
					key = "inline:" + scheme.ID + ":" + c.ID
				}
				path = append(path, key)
				ref = refs[c.Target]
			}
			if len(path) > 0 {
				result[entry.Ref] = strings.Join(path, "/")
			}
		}
		return result
	}
	current := routes(scheme.Layout)
	if len(current) == 0 {
		return nil
	}
	schemes, _, err := s.Repository.CanvasSchemes(ctx, 1, 1000000)
	if err != nil {
		return err
	}
	for _, other := range schemes {
		if other.ID == scheme.ID || other.Execution != "server" {
			continue
		}
		for entry, path := range routes(other.Layout) {
			if own, exists := current[entry]; exists && own != path {
				return errors.New("entry_routing_conflict: " + entry + " is routed differently by scheme " + other.ID + " (" + other.Name + ")")
			}
		}
	}
	return nil
}

func (r *Repository) markTunnelPublication(ctx context.Context, schemeID, batchID string, plan *canvasPlan) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE canvas_schemes SET published_at=?,published_batch_id=? WHERE id=? AND updated_at=?", timestamp(), batchID, schemeID, plan.schemeStamp); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM canvas_tunnel_publications WHERE scheme_id=?", schemeID); err != nil {
		return err
	}
	for tunnelID := range plan.schemeTunnelIDs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO canvas_tunnel_publications VALUES(?,?)", schemeID, tunnelID); err != nil {
			return err
		}
	}
	for tunnelID, version := range plan.tunnelVersions {
		if _, err := tx.ExecContext(ctx, "UPDATE canvas_tunnels SET published_version=? WHERE id=? AND updated_at=?", version, tunnelID, version); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) canvasPublicationUnchanged(ctx context.Context, p *canvasPlan) (bool, error) {
	if len(p.tunnelIDs) == 0 {
		return false, nil
	}
	for tunnelID := range p.tunnelIDs {
		t, err := s.Repository.canvasTunnel(ctx, tunnelID)
		if err != nil {
			return false, err
		}
		if t.publishedVersion != t.UpdatedAt {
			return false, nil
		}
	}
	for name, owner := range p.owners {
		kind, table := "inbound", "inbounds"
		if p.outbounds[name] {
			kind, table = "outbound", "outbounds"
		}
		if p.chainName[name] {
			kind, table = "chain", "proxy_chains"
		}
		var count int
		if err := s.Repository.Database.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" x JOIN canvas_tunnel_objects o ON o.object_id=x.id AND o.object_kind=? WHERE x.name=? AND o.tunnel_id=?", kind, name, owner).Scan(&count); err != nil {
			return false, err
		}
		if count != 1 {
			return false, nil
		}
	}
	unchanged, err := s.canvasObjectsUnchanged(ctx, p)
	if err != nil || !unchanged {
		return false, err
	}
	stale, err := s.canvasStaleObjects(ctx, p)
	if err != nil {
		return false, err
	}
	if len(stale.inbounds)+len(stale.outbounds)+len(stale.chains) > 0 {
		return false, nil
	}
	conflicts, err := s.canvasListenerConflicts(ctx, p, stale)
	if err != nil {
		return false, err
	}
	if len(conflicts) > 0 {
		return false, nil
	}
	for _, entry := range p.entries {
		target := entry.existing.DefaultOutbound
		if entry.chain != nil {
			c, err := s.Repository.canvasChainNamed(ctx, entry.chain.name)
			if err != nil {
				return false, nil
			}
			target = "chain:" + c.ID
		} else if entry.edge != nil {
			o, err := s.Repository.canvasOutboundNamed(ctx, entry.edge.sourceNode, entry.edge.outboundName)
			if err != nil {
				return false, nil
			}
			target = o.ID
		}
		if target != entry.existing.DefaultOutbound {
			return false, nil
		}
	}
	return true, nil
}

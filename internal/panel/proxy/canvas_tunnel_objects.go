package proxy

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (r *Repository) tunnelObjects(ctx context.Context, tunnelID string) ([]CanvasTunnelObject, error) {
	rows, err := r.Database.QueryContext(ctx, `SELECT o.object_kind,o.object_id,COALESCE(i.node_id,b.node_id,json_extract(c.node_ids_json,'$[0]'),''),COALESCE(i.name,b.name,c.name,''),COALESCE(i.port,0)
 FROM canvas_tunnel_objects o LEFT JOIN inbounds i ON o.object_kind='inbound' AND i.id=o.object_id LEFT JOIN outbounds b ON o.object_kind='outbound' AND b.id=o.object_id LEFT JOIN proxy_chains c ON o.object_kind='chain' AND c.id=o.object_id WHERE o.tunnel_id=? ORDER BY o.object_kind,o.object_id`, tunnelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CanvasTunnelObject{}
	for rows.Next() {
		var o CanvasTunnelObject
		if err := rows.Scan(&o.Kind, &o.ID, &o.NodeID, &o.Name, &o.Port); err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, rows.Err()
}
func (r *Repository) objectOwner(ctx context.Context, kind, objectID string) (string, error) {
	var owner string
	err := r.Database.QueryRowContext(ctx, "SELECT tunnel_id FROM canvas_tunnel_objects WHERE object_kind=? AND object_id=?", kind, objectID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return owner, err
}
func (s *Service) ownCanvasObject(ctx context.Context, plan *canvasPlan, kind, objectID, name string, undo *canvasUndo) error {
	owner := plan.owners[name]
	if owner == "" {
		return errors.New("canvas object has no tunnel owner")
	}
	old, err := s.Repository.objectOwner(ctx, kind, objectID)
	if err != nil {
		return err
	}
	if old != "" {
		if old != owner {
			return errors.New("canvas object belongs to another tunnel")
		}
		return nil
	}
	_, err = s.Repository.Database.ExecContext(ctx, "INSERT INTO canvas_tunnel_objects(tunnel_id,object_kind,object_id) VALUES(?,?,?)", owner, kind, objectID)
	if err == nil {
		undo.add(func(ctx context.Context) error {
			_, e := s.Repository.Database.ExecContext(ctx, "DELETE FROM canvas_tunnel_objects WHERE tunnel_id=? AND object_kind=? AND object_id=?", owner, kind, objectID)
			return e
		})
	}
	return err
}

// Scope includes current tunnels and previously published tunnels dropped by
// this drawing, but never a dropped tunnel still referenced by another drawing.
func (s *Service) canvasOwnedIDs(ctx context.Context, plan *canvasPlan, kind string) ([]string, error) {
	scope := map[string]bool{}
	for key := range plan.tunnelIDs {
		scope[key] = true
	}
	rows, err := s.Repository.Database.QueryContext(ctx, "SELECT tunnel_id FROM canvas_tunnel_publications WHERE scheme_id=?", plan.schemeID)
	if err != nil {
		return nil, err
	}
	old := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return nil, err
		}
		old = append(old, t)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, t := range old {
		refs, err := s.Repository.tunnelSchemes(ctx, t)
		if err != nil {
			return nil, err
		}
		external := false
		for _, ref := range refs {
			if ref.ID != plan.schemeID {
				external = true
			}
		}
		if !external {
			scope[t] = true
		}
	}
	result := []string{}
	for t := range scope {
		objects, err := s.Repository.tunnelObjects(ctx, t)
		if err != nil {
			return nil, err
		}
		for _, o := range objects {
			if o.Kind == kind {
				result = append(result, o.ID)
			}
		}
	}
	return result, nil
}
func (s *Service) canvasOwnedInbounds(ctx context.Context, p *canvasPlan) ([]Inbound, error) {
	ids, err := s.canvasOwnedIDs(ctx, p, "inbound")
	if err != nil {
		return nil, err
	}
	items := []Inbound{}
	for _, id := range ids {
		v, e := s.Repository.Inbound(ctx, id, true)
		if e != nil {
			return nil, e
		}
		items = append(items, v)
	}
	return items, nil
}
func (s *Service) canvasOwnedOutbounds(ctx context.Context, p *canvasPlan) ([]Outbound, error) {
	ids, err := s.canvasOwnedIDs(ctx, p, "outbound")
	if err != nil {
		return nil, err
	}
	items := []Outbound{}
	for _, id := range ids {
		v, e := s.Repository.Outbound(ctx, id, true)
		if e != nil {
			return nil, e
		}
		items = append(items, v)
	}
	return items, nil
}
func (s *Service) canvasOwnedChains(ctx context.Context, p *canvasPlan) ([]Chain, error) {
	ids, err := s.canvasOwnedIDs(ctx, p, "chain")
	if err != nil {
		return nil, err
	}
	items := []Chain{}
	for _, id := range ids {
		v, e := s.Repository.Chain(ctx, id)
		if e != nil {
			return nil, e
		}
		items = append(items, v)
	}
	return items, nil
}

func legacyCanvasName(kind, name string) bool {
	if !strings.HasPrefix(name, "[") || !strings.Contains(name, "] ") {
		return false
	}
	switch kind {
	case "inbound":
		return strings.HasSuffix(name, canvasInboundSuffix)
	case "outbound":
		return strings.HasSuffix(name, canvasOutboundSuffix)
	case "chain":
		return strings.Contains(name, canvasChainMarker)
	}
	return false
}
func (r *Repository) LegacyCanvasObjects(ctx context.Context) ([]CanvasTunnelObject, error) {
	if err := r.ReconcileCanvasTunnels(ctx); err != nil {
		return nil, err
	}
	rows, err := r.Database.QueryContext(ctx, `SELECT 'inbound',i.id,i.node_id,i.name,i.port FROM inbounds i WHERE (i.internal_relay=1 OR i.protocol='forward') AND NOT EXISTS(SELECT 1 FROM canvas_tunnel_objects o WHERE o.object_kind='inbound' AND o.object_id=i.id)
 UNION ALL SELECT 'outbound',b.id,b.node_id,b.name,0 FROM outbounds b WHERE b.target_inbound_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM canvas_tunnel_objects o WHERE o.object_kind='outbound' AND o.object_id=b.id)
 UNION ALL SELECT 'chain',c.id,COALESCE(json_extract(c.node_ids_json,'$[0]'),''),c.name,0 FROM proxy_chains c WHERE NOT EXISTS(SELECT 1 FROM canvas_tunnel_objects o WHERE o.object_kind='chain' AND o.object_id=c.id) ORDER BY 1,2`)
	if err != nil {
		return nil, err
	}
	items := []CanvasTunnelObject{}
	for rows.Next() {
		var o CanvasTunnelObject
		if err := rows.Scan(&o.Kind, &o.ID, &o.NodeID, &o.Name, &o.Port); err != nil {
			rows.Close()
			return nil, err
		}
		if legacyCanvasName(o.Kind, o.Name) {
			o.CandidateTunnels = []string{}
			items = append(items, o)
		}
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	tunnels, err := r.CanvasTunnels(ctx)
	if err != nil {
		return nil, err
	}
	for n := range items {
		for _, t := range tunnels {
			if r.compatibleTunnelObject(ctx, t, items[n]) == nil {
				items[n].CandidateTunnels = append(items[n].CandidateTunnels, t.ID)
			}
		}
	}
	return items, nil
}

func (r *Repository) compatibleTunnelObject(ctx context.Context, t CanvasTunnel, o CanvasTunnelObject) error {
	return compatibleCanvasObject(ctx, r.Database, t, o)
}

func (r *Repository) AdoptCanvasObjects(ctx context.Context, tunnelID string, objects []CanvasTunnelObject) error {
	if err := r.ReconcileCanvasTunnels(ctx); err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := scanTunnel(tx.QueryRowContext(ctx, "SELECT "+tunnelColumns+" FROM canvas_tunnels WHERE id=?", tunnelID))
	if err != nil {
		return err
	}
	if len(objects) == 0 || len(objects) > 100 {
		return errors.New("objects must contain 1 to 100 entries")
	}
	seen := map[string]bool{}
	for _, o := range objects {
		key := o.Kind + ":" + o.ID
		if seen[key] {
			return errors.New("duplicate object")
		}
		seen[key] = true
		if err := compatibleCanvasObject(ctx, tx, t, o); err != nil {
			return err
		}
	}
	for _, o := range objects {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM canvas_tunnel_objects WHERE object_kind=? AND object_id=?", o.Kind, o.ID).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrCanvasObjectOwned
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO canvas_tunnel_objects VALUES(?,?,?)", tunnelID, o.Kind, o.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

var ErrCanvasObjectOwned = errors.New("object already belongs to a tunnel")

type TunnelInUseError struct{ Schemes []CanvasSchemeRef }

func (e *TunnelInUseError) Error() string { return "tunnel_in_use" }
func (s *Service) DeleteCanvasTunnel(ctx context.Context, tunnelID string) error {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	if err := s.Repository.ReconcileCanvasTunnels(ctx); err != nil {
		return err
	}
	if _, err := s.Repository.canvasTunnel(ctx, tunnelID); err != nil {
		return err
	}
	refs, err := s.Repository.tunnelSchemes(ctx, tunnelID)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return &TunnelInUseError{refs}
	}
	p := &canvasPlan{tunnelIDs: map[string]bool{tunnelID: true}, owners: map[string]string{}, inbounds: map[string]bool{}, outbounds: map[string]bool{}, chainName: map[string]bool{}}
	stale, err := s.canvasStaleObjects(ctx, p)
	if err != nil {
		return err
	}
	if err := s.canvasCheckReferences(ctx, p, stale); err != nil {
		return err
	}
	if warnings := s.canvasRemoveStale(ctx, stale); len(warnings) > 0 {
		return errors.New(strings.Join(warnings, "; "))
	}
	_, err = s.Repository.Database.ExecContext(ctx, "DELETE FROM canvas_tunnels WHERE id=?", tunnelID)
	return err
}

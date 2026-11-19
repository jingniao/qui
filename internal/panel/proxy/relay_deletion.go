package proxy

import (
	"context"
	"database/sql"
	"log/slog"
	"proxycontrol/internal/panel/node"
	"sort"
)

type InboundDeleteResult struct {
	PendingNodes []RelayCleanupNode `json:"pendingNodes"`
}

type relayDeletionPlan struct {
	preview                       node.DeletionImpact
	removed                       map[string]bool
	schemes                       map[string]CanvasRemovePlan
	rules, tunnels, canvasTunnels map[string]bool
	objects                       map[string]CanvasRemoveObject
	nodes                         map[string]bool
	guards                        []canvasRemovalRow
}

// Configuration snapshots are captured before calculating the graph and checked
// inside the write transaction. No telemetry/history tables participate.
func (s *Service) relayDeletionGuards(ctx context.Context) ([]canvasRemovalRow, error) {
	guards := []canvasRemovalRow{}
	for _, table := range []string{"inbounds", "outbounds", "outbound_groups", "outbound_group_members", "proxy_chains", "proxy_chain_hops", "canvas_schemes", "canvas_entry_bindings", "canvas_tunnels", "canvas_tunnel_objects", "canvas_tunnel_publications", "tunnels", "tunnel_hops", "tunnel_rules", "tunnel_objects", "port_claims", "policies", "deployment_batches", "deployment_batch_items"} {
		query := "SELECT * FROM " + table + " WHERE ?='' ORDER BY rowid"
		values, err := readCanvasRemovalRows(ctx, s.Repository.Database, query, "")
		if err != nil {
			return nil, err
		}
		guards = append(guards, canvasRemovalRow{query: query, values: values})
	}
	return guards, nil
}

func (s *Service) prepareRelayDeletion(ctx context.Context, inboundID, nodeID string) (relayDeletionPlan, error) {
	p := relayDeletionPlan{preview: node.DeletionImpact{Schemes: []node.DeletionItem{}, Tunnels: []node.DeletionItem{}, Rules: []node.DeletionItem{}, Inbounds: []node.DeletionInbound{}, Blockers: []node.Blocker{}}, removed: map[string]bool{}, schemes: map[string]CanvasRemovePlan{}, rules: map[string]bool{}, tunnels: map[string]bool{}, canvasTunnels: map[string]bool{}, objects: map[string]CanvasRemoveObject{}, nodes: map[string]bool{}}
	var err error
	p.guards, err = s.relayDeletionGuards(ctx)
	if err != nil {
		return p, err
	}
	db := s.Repository.Database
	addIDs := func(query string, args []any, fn func(string)) error {
		rows, e := db.QueryContext(ctx, query, args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return e
			}
			fn(id)
		}
		return rows.Err()
	}
	if inboundID != "" {
		if _, err = s.Repository.Inbound(ctx, inboundID, true); err != nil {
			return p, err
		}
		p.removed["inbound:"+inboundID] = true
	}
	if nodeID != "" {
		if err = addIDs("SELECT id FROM inbounds WHERE node_id=?", []any{nodeID}, func(id string) { p.removed["inbound:"+id] = true }); err != nil {
			return p, err
		}
		if err = addIDs("SELECT tunnel_id FROM tunnel_hops WHERE node_id=?", []any{nodeID}, func(id string) { p.tunnels[id] = true }); err != nil {
			return p, err
		}
	}
	// Expand until deleting generated listeners can no longer reach another rule
	// or a drawing that uses them. All selected consumers are excluded together.
	for {
		before := len(p.removed) + len(p.rules) + len(p.tunnels) + len(p.schemes)
		if err = addIDs(`SELECT DISTINCT s.id FROM canvas_schemes s,json_each(s.layout_json,'$.instances') i WHERE
   (?<>'' AND (json_extract(i.value,'$.ref')='server:'||? OR json_extract(i.value,'$.ref') IN (SELECT 'outbound:'||id FROM outbounds WHERE node_id=?)))`, []any{nodeID, nodeID, nodeID}, func(id string) { p.removed["scheme:"+id] = true }); err != nil {
			return p, err
		}
		for _, key := range sortedDeletionKeys(p.removed) {
			if len(key) < 8 || key[:8] != "inbound:" {
				continue
			}
			id := key[8:]
			if err = addIDs(`SELECT id FROM canvas_schemes s WHERE EXISTS(SELECT 1 FROM json_each(s.layout_json,'$.instances') i WHERE json_extract(i.value,'$.ref')='inbound:'||?1) UNION SELECT scheme_id FROM canvas_entry_bindings WHERE inbound_id=?1 AND scheme_id IS NOT NULL`, []any{id}, func(id string) { p.removed["scheme:"+id] = true }); err != nil {
				return p, err
			}
			if err = addIDs("SELECT id FROM tunnel_rules WHERE target_inbound_id=?", []any{id}, func(id string) { p.rules[id] = true }); err != nil {
				return p, err
			}
		}
		if nodeID != "" {
			if err = addIDs("SELECT id FROM tunnel_rules WHERE entry_node_id=?", []any{nodeID}, func(id string) { p.rules[id] = true }); err != nil {
				return p, err
			}
		}
		for id := range p.tunnels {
			if err = addIDs("SELECT id FROM tunnel_rules WHERE tunnel_id=?", []any{id}, func(id string) { p.rules[id] = true }); err != nil {
				return p, err
			}
		}
		// A tunnel only becomes empty when at least one of its rules is deleted.
		for id := range p.rules {
			p.removed["tunnel_rule:"+id] = true
			var tunnel sql.NullString
			if err = db.QueryRowContext(ctx, "SELECT tunnel_id FROM tunnel_rules WHERE id=?", id).Scan(&tunnel); err != nil {
				return p, err
			}
			if tunnel.Valid {
				all := true
				if err = addIDs("SELECT id FROM tunnel_rules WHERE tunnel_id=?", []any{tunnel.String}, func(id string) {
					if !p.rules[id] {
						all = false
					}
				}); err != nil {
					return p, err
				}
				if all {
					p.tunnels[tunnel.String] = true
				}
			}
		}
		for id := range p.rules {
			if err = addIDs("SELECT object_id FROM tunnel_objects WHERE owner_kind='rule' AND owner_id=?", []any{id}, func(id string) { p.removed["inbound:"+id] = true }); err != nil {
				return p, err
			}
		}
		for id := range p.tunnels {
			if err = addIDs("SELECT object_id FROM tunnel_objects WHERE owner_kind='tunnel' AND owner_id=?", []any{id}, func(id string) { p.removed["inbound:"+id] = true }); err != nil {
				return p, err
			}
		}
		for _, key := range sortedDeletionKeys(p.removed) {
			if len(key) < 7 || key[:7] != "scheme:" {
				continue
			}
			id := key[7:]
			scheme, e := s.Repository.CanvasScheme(ctx, id)
			if e != nil {
				return p, e
			}
			scope, e := s.canvasRemovalScopeExcluding(ctx, id, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt}, p.removed)
			if e != nil {
				return p, e
			}
			p.schemes[id] = scope
			for _, entry := range scope.Entries {
				p.removed["inbound:"+entry.InboundID] = true
			}
			for _, o := range scope.Objects {
				if !o.Keep {
					p.objects[o.Kind+":"+o.ID] = o
					p.removed[o.Kind+":"+o.ID] = true
				}
			}
			for _, id := range scope.unsharedTunnelIDs {
				p.canvasTunnels[id] = true
			}
			for _, id := range scope.Nodes {
				p.nodes[id] = true
			}
		}
		if before == len(p.removed)+len(p.rules)+len(p.tunnels)+len(p.schemes) {
			break
		}
	}
	for _, key := range sortedDeletionKeys(p.removed) {
		if len(key) < 8 || key[:8] != "inbound:" {
			continue
		}
		id := key[8:]
		var item node.DeletionInbound
		var nid string
		if err = db.QueryRowContext(ctx, "SELECT i.id,i.name,n.name,i.node_id FROM inbounds i JOIN nodes n ON n.id=i.node_id WHERE i.id=?", id).Scan(&item.ID, &item.Name, &item.NodeName, &nid); err != nil {
			return p, err
		}
		p.nodes[nid] = true
		if id != inboundID {
			p.preview.Inbounds = append(p.preview.Inbounds, item)
		}
		refs, e := s.Repository.canvasReferrers(ctx, id)
		if e != nil {
			return p, e
		}
		for _, ref := range refs {
			if !p.removed[ref.Kind+":"+ref.ID] {
				kind := ref.Kind
				if kind == "chain" {
					kind = "proxy_chain"
				}
				if kind == "group" {
					kind = "outbound_group"
				}
				p.preview.Blockers = append(p.preview.Blockers, node.Blocker{Kind: kind, ID: ref.ID, Name: ref.Name})
			}
		}
	}
	for _, id := range sortedDeletionKeys(p.schemes) {
		var name string
		if err = db.QueryRowContext(ctx, "SELECT name FROM canvas_schemes WHERE id=?", id).Scan(&name); err != nil {
			return p, err
		}
		p.preview.Schemes = append(p.preview.Schemes, node.DeletionItem{ID: id, Name: name})
	}
	for _, id := range sortedDeletionKeys(p.rules) {
		var item node.DeletionItem
		var nid string
		if err = db.QueryRowContext(ctx, "SELECT id,name,entry_node_id FROM tunnel_rules WHERE id=?", id).Scan(&item.ID, &item.Name, &nid); err != nil {
			return p, err
		}
		p.preview.Rules = append(p.preview.Rules, item)
		p.nodes[nid] = true
		if err = addIDs("SELECT node_id FROM port_claims WHERE owner_kind='rule' AND owner_id=?", []any{id}, func(id string) { p.nodes[id] = true }); err != nil {
			return p, err
		}
	}
	for _, id := range sortedDeletionKeys(p.tunnels) {
		var item node.DeletionItem
		if err = db.QueryRowContext(ctx, "SELECT id,name FROM tunnels WHERE id=?", id).Scan(&item.ID, &item.Name); err != nil {
			return p, err
		}
		p.preview.Tunnels = append(p.preview.Tunnels, item)
		if err = addIDs("SELECT node_id FROM tunnel_hops WHERE tunnel_id=?", []any{id}, func(id string) { p.nodes[id] = true }); err != nil {
			return p, err
		}
	}
	// A not-yet-finalized tunnel publication can restore its before-image even
	// after the Agent batch has finished. Keep its reservation until finalization.
	if nodeID != "" {
		p.nodes[nodeID] = true
	}
	batches := map[string]bool{}
	for _, nid := range sortedDeletionKeys(p.nodes) {
		rows, e := db.QueryContext(ctx, `SELECT DISTINCT b.id,b.name FROM deployment_batches b WHERE
 (b.status IN ('deploying','rolling_back','rollback_failed') AND EXISTS(SELECT 1 FROM deployment_batch_items i WHERE i.batch_id=b.id AND i.node_id=?1))
 OR (b.tunnel_finalized=0 AND EXISTS(SELECT 1 FROM json_each(b.tunnel_publication_json,'$.nodes') n WHERE n.value=?1)) ORDER BY b.id`, nid)
		if e != nil {
			return p, e
		}
		for rows.Next() {
			var item node.Blocker
			item.Kind = "deployment_batch"
			if e = rows.Scan(&item.ID, &item.Name); e != nil {
				rows.Close()
				return p, e
			}
			if !batches[item.ID] {
				p.preview.Blockers = append(p.preview.Blockers, item)
				batches[item.ID] = true
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return p, e
		}
	}
	return p, nil
}

func sortedDeletionKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for id := range m {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	return keys
}

func (s *Service) applyRelayDeletion(ctx context.Context, tx *sql.Tx, p relayDeletionPlan) error {
	if err := checkCanvasRemovalRows(ctx, tx, p.guards); err != nil {
		return err
	}
	exec := func(query, id string) error { _, err := tx.ExecContext(ctx, query, id); return err }
	for _, id := range sortedDeletionKeys(p.schemes) {
		if err := exec("DELETE FROM canvas_schemes WHERE id=?", id); err != nil {
			return err
		}
	}
	for _, id := range sortedDeletionKeys(p.rules) {
		if err := exec("DELETE FROM port_claims WHERE owner_kind='rule' AND owner_id=?", id); err != nil {
			return err
		}
		if err := exec("DELETE FROM tunnel_rules WHERE id=?", id); err != nil {
			return err
		}
	}
	for _, key := range sortedDeletionKeys(p.removed) {
		if len(key) > 8 && key[:8] == "inbound:" {
			if err := exec("DELETE FROM canvas_entry_bindings WHERE inbound_id=?", key[8:]); err != nil {
				return err
			}
		}
	}
	objects := []CanvasRemoveObject{}
	for _, key := range sortedDeletionKeys(p.objects) {
		objects = append(objects, p.objects[key])
	}
	// Include the explicitly requested and tunnel-managed listeners so orphaned
	// internal relay credentials receive the same cleanup as canvas objects.
	for _, key := range sortedDeletionKeys(p.removed) {
		if len(key) > 8 && key[:8] == "inbound:" {
			if _, ok := p.objects[key]; !ok {
				objects = append(objects, CanvasRemoveObject{CanvasTunnelObject: CanvasTunnelObject{Kind: "inbound", ID: key[8:]}})
			}
		}
	}
	if err := deleteCanvasCleanupObjects(ctx, tx, objects); err != nil {
		return err
	}
	for _, id := range sortedDeletionKeys(p.canvasTunnels) {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM canvas_tunnel_objects WHERE tunnel_id=?", id).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if err := exec("DELETE FROM port_claims WHERE owner_kind='canvas' AND owner_id=?", id); err != nil {
				return err
			}
			if err := deleteCanvasTunnelRow(ctx, tx, id); err != nil {
				return err
			}
		}
	}
	for _, id := range sortedDeletionKeys(p.tunnels) {
		if err := exec("DELETE FROM port_claims WHERE owner_kind='tunnel_hop' AND owner_id=?", id); err != nil {
			return err
		}
		if err := exec("DELETE FROM tunnels WHERE id=?", id); err != nil {
			return err
		}
	}
	for _, id := range sortedDeletionKeys(p.nodes) {
		if err := registerRelayCleanup(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deletionPendingNodes(ctx context.Context, p relayDeletionPlan) ([]RelayCleanupNode, error) {
	if s.RelayCleanupRequested != nil {
		if err := s.RelayCleanupRequested(ctx); err != nil {
			slog.Error("relay cleanup dispatch failed", "error", err)
		}
	}
	result := []RelayCleanupNode{}
	for _, id := range sortedDeletionKeys(p.nodes) {
		var item RelayCleanupNode
		var status, code string
		err := s.Repository.Database.QueryRowContext(ctx, `SELECT n.id,n.name,r.reason,n.status,r.last_code FROM relay_cleanup_nodes r JOIN nodes n ON n.id=r.node_id WHERE n.id=? AND n.deleted_at IS NULL`, id).Scan(&item.NodeID, &item.Name, &item.Reason, &status, &code)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		if status != "online" && status != "degraded" || code != "" {
			result = append(result, item)
		}
	}
	return result, nil
}

func (s *Service) PreviewInboundDeletion(ctx context.Context, id string) (node.DeletionImpact, error) {
	if err := s.Repository.refuseManagedInbound(ctx, id); err != nil {
		return node.DeletionImpact{}, err
	}
	p, err := s.prepareRelayDeletion(ctx, id, "")
	return p.preview, err
}
func (s *Service) DeleteInbound(ctx context.Context, id string) (InboundDeleteResult, error) {
	s.canvasMu.Lock()
	result := InboundDeleteResult{PendingNodes: []RelayCleanupNode{}}
	if err := s.Repository.refuseManagedInbound(ctx, id); err != nil {
		s.canvasMu.Unlock()
		return result, err
	}
	p, err := s.prepareRelayDeletion(ctx, id, "")
	if err == nil && len(p.preview.Blockers) > 0 {
		err = &tunnelError{Code: "inbound_delete_blocked", Status: 409, Message: "入站仍被其他配置引用，请先处理以下引用", Details: map[string]any{"blockers": p.preview.Blockers}}
	}
	if err == nil {
		var tx *sql.Tx
		tx, err = s.Repository.Database.BeginTx(ctx, nil)
		if err == nil {
			err = s.applyRelayDeletion(ctx, tx, p)
			if err == nil {
				err = s.Repository.commitDraft(tx)
			}
			tx.Rollback()
		}
	}
	s.canvasMu.Unlock()
	if err != nil {
		return result, err
	}
	result.PendingNodes, err = s.deletionPendingNodes(ctx, p)
	return result, err
}

func (s *Service) PrepareNodeDeletion(ctx context.Context, id string) (node.DeletionCascade, error) {
	p, err := s.prepareRelayDeletion(ctx, "", id)
	if err != nil {
		return node.DeletionCascade{}, err
	}
	// Node blocker kinds use public names; A1 uses chain/group internally.
	removed := map[string]bool{}
	for key, v := range p.removed {
		removed[key] = v
	}
	for _, o := range p.objects {
		if o.Kind == "chain" {
			removed["proxy_chain:"+o.ID] = true
		}
		if o.Kind == "outbound" {
			removed["outbound_target:"+o.ID] = true
		}
	}
	return node.DeletionCascade{Preview: p.preview, Removed: removed, Apply: func(ctx context.Context, tx *sql.Tx) error { return s.applyRelayDeletion(ctx, tx, p) }, AfterCommit: func(ctx context.Context) ([]node.CleanupPendingNode, error) {
		s.Repository.notifyDraft()
		items, err := s.deletionPendingNodes(ctx, p)
		out := []node.CleanupPendingNode{}
		for _, i := range items {
			out = append(out, node.CleanupPendingNode{NodeID: i.NodeID, Name: i.Name, Reason: i.Reason})
		}
		return out, err
	}}, nil
}

package proxy

import "context"

// ReconcileRelayCleanup removes old detached resources exactly once. The marker,
// object deletion and durable node work are committed in the same transaction.
// Restoring an archive preserves its marker; an uncompleted archive is reconciled
// by the newly constructed panel service in the same way as normal startup.
func (s *Service) ReconcileRelayCleanup(ctx context.Context) error {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var completed bool
	if err = tx.QueryRowContext(ctx, "SELECT completed FROM relay_cleanup_reconcile WHERE id=1").Scan(&completed); err != nil {
		return err
	}
	if completed {
		return nil
	}
	nodes := map[string]bool{}
	entries := []string{}
	rows, err := tx.QueryContext(ctx, "SELECT b.inbound_id,i.node_id FROM canvas_entry_bindings b JOIN inbounds i ON i.id=b.inbound_id WHERE b.state='detached'")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, nodeID string
		if err = rows.Scan(&id, &nodeID); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, id)
		nodes[nodeID] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	tunnels := []string{}
	rows, err = tx.QueryContext(ctx, `SELECT t.id FROM canvas_tunnels t WHERE NOT EXISTS
(SELECT 1 FROM canvas_schemes s,json_each(s.layout_json,'$.connections') c WHERE s.execution='server' AND json_extract(c.value,'$.tunnelId')=t.id)
AND NOT EXISTS (SELECT 1 FROM canvas_tunnel_publications p WHERE p.tunnel_id=t.id) ORDER BY t.id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		tunnels = append(tunnels, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	objects := []CanvasRemoveObject{}
	for _, tunnelID := range tunnels {
		rows, err = tx.QueryContext(ctx, `SELECT o.object_kind,o.object_id,COALESCE(i.node_id,b.node_id,json_extract(c.node_ids_json,'$[0]'),'')
FROM canvas_tunnel_objects o LEFT JOIN inbounds i ON o.object_kind='inbound' AND i.id=o.object_id LEFT JOIN outbounds b ON o.object_kind='outbound' AND b.id=o.object_id LEFT JOIN proxy_chains c ON o.object_kind='chain' AND c.id=o.object_id WHERE o.tunnel_id=?`, tunnelID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var object CanvasRemoveObject
			if err = rows.Scan(&object.Kind, &object.ID, &object.NodeID); err != nil {
				rows.Close()
				return err
			}
			objects = append(objects, object)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	// The same ownership rule as normal removal: external users keep an object
	// and all generated providers it still needs. Never claim by display name.
	candidates := map[string]int{}
	detached := map[string]bool{}
	for i, object := range objects {
		candidates[object.Kind+":"+object.ID] = i
	}
	for _, id := range entries {
		detached[id] = true
	}
	for changed := true; changed; {
		changed = false
		for i := range objects {
			object := &objects[i]
			if object.Keep {
				continue
			}
			rows, err = tx.QueryContext(ctx, canvasReferrersQuery, object.ID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var kind, id, name string
				if err = rows.Scan(&kind, &id, &name); err != nil {
					rows.Close()
					return err
				}
				if kind == "inbound" && detached[id] {
					continue
				}
				if index, ok := candidates[kind+":"+id]; ok && !objects[index].Keep {
					continue
				}
				object.Keep = true
			}
			if err = rows.Err(); err != nil {
				rows.Close()
				return err
			}
			if err = rows.Close(); err != nil {
				return err
			}
			var external bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM policies WHERE json_extract(action_json,'$.target_id')=?1 OR json_extract(action_json,'$.backup_id')=?1)
OR EXISTS(SELECT 1 FROM canvas_schemes s,json_each(s.layout_json,'$.instances') i WHERE json_extract(i.value,'$.ref')=?2)
OR EXISTS(SELECT 1 FROM tunnel_rules WHERE ?3='inbound' AND target_inbound_id=?1)`, object.ID, object.Kind+":"+object.ID, object.Kind).Scan(&external); err != nil {
				return err
			}
			object.Keep = object.Keep || external
			if object.Keep {
				changed = true
			}
		}
	}
	for _, object := range objects {
		if !object.Keep && object.NodeID != "" {
			nodes[object.NodeID] = true
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM canvas_entry_bindings WHERE state='detached'"); err != nil {
		return err
	}
	if err = deleteCanvasCleanupObjects(ctx, tx, objects); err != nil {
		return err
	}
	for _, id := range entries {
		if _, err = tx.ExecContext(ctx, "DELETE FROM inbounds WHERE id=?", id); err != nil {
			return err
		}
	}
	for _, id := range tunnels {
		var remaining int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM canvas_tunnel_objects WHERE tunnel_id=?", id).Scan(&remaining); err != nil {
			return err
		}
		if remaining == 0 {
			if err = deleteCanvasTunnelRow(ctx, tx, id); err != nil {
				return err
			}
		}
	}
	for id := range nodes {
		if err = registerRelayCleanup(ctx, tx, id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE relay_cleanup_reconcile SET completed=1 WHERE id=1"); err != nil {
		return err
	}
	return tx.Commit()
}

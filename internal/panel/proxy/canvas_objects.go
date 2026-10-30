package proxy

import (
	"context"
	"database/sql"
	"errors"
)

// Named lookups require explicit ownership; matching a legacy name never adopts it.

// canvasInboundNamed returns the generated inbound of one node by exact name.
func (r *Repository) canvasInboundNamed(ctx context.Context, nodeID, name string) (Inbound, error) {
	var id string
	err := r.Database.QueryRowContext(ctx, "SELECT id FROM inbounds WHERE node_id=? AND name=? AND EXISTS(SELECT 1 FROM canvas_tunnel_objects o WHERE o.object_kind='inbound' AND o.object_id=inbounds.id) AND (internal_relay=1 OR protocol='forward') ORDER BY id LIMIT 1", nodeID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Inbound{}, ErrNotFound
	}
	if err != nil {
		return Inbound{}, err
	}
	return r.Inbound(ctx, id, true)
}

func (r *Repository) canvasOutboundNamed(ctx context.Context, nodeID, name string) (Outbound, error) {
	var id string
	err := r.Database.QueryRowContext(ctx, "SELECT id FROM outbounds WHERE node_id=? AND name=? AND EXISTS(SELECT 1 FROM canvas_tunnel_objects o WHERE o.object_kind='outbound' AND o.object_id=outbounds.id) AND target_inbound_id IS NOT NULL ORDER BY id LIMIT 1", nodeID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Outbound{}, ErrNotFound
	}
	if err != nil {
		return Outbound{}, err
	}
	return r.Outbound(ctx, id, true)
}

func (r *Repository) canvasChainNamed(ctx context.Context, name string) (Chain, error) {
	var id string
	err := r.Database.QueryRowContext(ctx, "SELECT id FROM proxy_chains WHERE name=? AND EXISTS(SELECT 1 FROM canvas_tunnel_objects o WHERE o.object_kind='chain' AND o.object_id=proxy_chains.id) ORDER BY id LIMIT 1", name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Chain{}, ErrNotFound
	}
	if err != nil {
		return Chain{}, err
	}
	return r.Chain(ctx, id)
}

// canvasInboundsAtPort lists every inbound of a node on that port, enabled or
// not, so publishing can tell its own listener from somebody else's.
func (r *Repository) canvasInboundsAtPort(ctx context.Context, nodeID string, port int) ([]Inbound, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM inbounds WHERE node_id=? AND port=? ORDER BY id", nodeID, port)
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
	items := []Inbound{}
	for _, id := range ids {
		inbound, err := r.Inbound(ctx, id, true)
		if err != nil {
			return nil, err
		}
		items = append(items, inbound)
	}
	return items, nil
}

// canvasReferrer is a proxy object that points at another one.
type canvasReferrer struct{ Kind, ID, Name string }

// canvasReferrers lists the proxy objects that use the object with this id: as
// a default outbound, a backup, a group member, a chain hop or final outbound,
// or a relay target. Routing policies are checked by checkPolicyReference.
func (r *Repository) canvasReferrers(ctx context.Context, id string) ([]canvasReferrer, error) {
	rows, err := r.Database.QueryContext(ctx, `SELECT 'inbound', id, name FROM inbounds WHERE default_outbound IN (?1, 'chain:' || ?1)
UNION ALL SELECT 'group', id, name FROM outbound_groups WHERE backup_outbound_id = ?1
UNION ALL SELECT 'group', g.id, g.name FROM outbound_group_members m JOIN outbound_groups g ON g.id = m.group_id WHERE m.outbound_id = ?1
UNION ALL SELECT 'outbound', id, name FROM outbounds WHERE backup_outbound_id = ?1 OR target_inbound_id = ?1
UNION ALL SELECT 'chain', id, name FROM proxy_chains WHERE final_outbound_id = ?1 OR backup_target_id = ?1
UNION ALL SELECT 'chain', c.id, c.name FROM proxy_chain_hops h JOIN proxy_chains c ON c.id = h.chain_id WHERE h.outbound_id = ?1 OR h.target_inbound_id = ?1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []canvasReferrer{}
	for rows.Next() {
		var item canvasReferrer
		if err := rows.Scan(&item.Kind, &item.ID, &item.Name); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// canvasSchemeNameTaken reports whether another scheme already uses the name.
// Publishing derives every generated object's name from it, so two drawings with
// the same name would prune each other's objects.
func (r *Repository) canvasSchemeNameTaken(ctx context.Context, name, exceptID string) (bool, error) {
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM canvas_schemes WHERE name=? AND id<>?", name, exceptID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

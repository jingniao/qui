package proxy

import (
	"context"
	"database/sql"
	"errors"
)

// The relay canvas generates inbounds, outbounds and chains whose names start
// with "[<scheme name>] ". These helpers are the only place that looks those
// objects up by name, so publishing can find, update and prune them without a
// second table naming them.

// canvasGenerated is the shape of each generated object besides its name: the
// generated suffix (or chain marker) and, for inbounds and outbounds, the relay
// role. An object the Owner created by hand under a similar name is therefore
// never updated or pruned by a publication.
var canvasGenerated = map[string]string{
	"inbounds":     "(internal_relay=1 OR protocol='forward') AND substr(name, -length(?2)) = ?2",
	"outbounds":    "target_inbound_id IS NOT NULL AND substr(name, -length(?2)) = ?2",
	"proxy_chains": "instr(name, ?2) > 0",
}

var canvasGeneratedMarker = map[string]string{"inbounds": canvasInboundSuffix, "outbounds": canvasOutboundSuffix, "proxy_chains": canvasChainMarker}

// canvasPrefixRows returns the ids of the generated objects of one proxy table
// whose name starts with the prefix. instr() keeps a '%' or '_' inside an
// Owner-chosen scheme name from behaving like a LIKE wildcard; scheme names
// cannot contain brackets, so one scheme's prefix never matches another's.
func (r *Repository) canvasPrefixRows(ctx context.Context, table, prefix string) ([]string, error) {
	shape, ok := canvasGenerated[table]
	if !ok {
		return nil, errors.New("invalid proxy table")
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM "+table+" WHERE instr(name, ?1) = 1 AND "+shape+" ORDER BY id LIMIT 512", prefix, canvasGeneratedMarker[table])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) canvasInboundsByPrefix(ctx context.Context, prefix string) ([]Inbound, error) {
	ids, err := r.canvasPrefixRows(ctx, "inbounds", prefix)
	if err != nil {
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

func (r *Repository) canvasOutboundsByPrefix(ctx context.Context, prefix string) ([]Outbound, error) {
	ids, err := r.canvasPrefixRows(ctx, "outbounds", prefix)
	if err != nil {
		return nil, err
	}
	items := []Outbound{}
	for _, id := range ids {
		outbound, err := r.Outbound(ctx, id, true)
		if err != nil {
			return nil, err
		}
		items = append(items, outbound)
	}
	return items, nil
}

func (r *Repository) canvasChainsByPrefix(ctx context.Context, prefix string) ([]Chain, error) {
	ids, err := r.canvasPrefixRows(ctx, "proxy_chains", prefix)
	if err != nil {
		return nil, err
	}
	items := []Chain{}
	for _, id := range ids {
		chain, err := r.Chain(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, chain)
	}
	return items, nil
}

// canvasInboundNamed returns the generated inbound of one node by exact name.
func (r *Repository) canvasInboundNamed(ctx context.Context, nodeID, name string) (Inbound, error) {
	var id string
	err := r.Database.QueryRowContext(ctx, "SELECT id FROM inbounds WHERE node_id=? AND name=? AND (internal_relay=1 OR protocol='forward') ORDER BY id LIMIT 1", nodeID, name).Scan(&id)
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
	err := r.Database.QueryRowContext(ctx, "SELECT id FROM outbounds WHERE node_id=? AND name=? AND target_inbound_id IS NOT NULL ORDER BY id LIMIT 1", nodeID, name).Scan(&id)
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
	err := r.Database.QueryRowContext(ctx, "SELECT id FROM proxy_chains WHERE name=? ORDER BY id LIMIT 1", name).Scan(&id)
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

// MarkCanvasPublished records the publication a drawing's objects belong to.
// The batch id is empty when the scheme published through a single revision.
func (r *Repository) MarkCanvasPublished(ctx context.Context, schemeID, batchID string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE canvas_schemes SET published_at=?,published_batch_id=? WHERE id=?", timestamp(), batchID, schemeID)
	return err
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

package proxy

import (
	"context"
	"errors"
)

// Legacy prefix helpers are test-fixture inventory only. Production ownership
// queries use canvas_tunnel_objects exclusively.
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

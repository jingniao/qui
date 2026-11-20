package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"proxycontrol/internal/panel/subscription/availability"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
)

// ValidateSubscriptionDependencies accepts pre-index archives only when their
// paths can be reconstructed from retained successful publication evidence.
func ValidateSubscriptionDependencies(ctx context.Context, database *sql.DB, key *vault.Vault) error {
	r := &Repository{Database: database, Vault: key}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var indexApplied int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version='0158_subscription_route_dependencies.sql'`).Scan(&indexApplied); err != nil {
		return err
	}
	if indexApplied == 1 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM subscription_route_dependencies`).Scan(&count); err != nil {
			return err
		}
	}
	graph, err := availability.LoadGraph(ctx, tx, key)
	if err != nil {
		return err
	}
	for _, state := range graph.States {
		for _, in := range state.Inbounds {
			if !in.Enabled || !in.SubscriptionVisible {
				continue
			}
			if _, err = graph.Route(in.ID); err != nil {
				return err
			}
			if indexApplied == 0 {
				continue
			}
			record, e := availability.Read(ctx, tx, "inbound:"+in.ID)
			if errors.Is(e, sql.ErrNoRows) {
				continue
			}
			if e != nil {
				return e
			}
			digest, e := availability.Digest(state)
			if e != nil {
				return e
			}
			if record.SourceDigest != digest || record.OwnerNodeID != state.NodeID {
				return fmt.Errorf("subscription inbound %s: index publication proof mismatch", in.ID)
			}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,published_snapshot FROM tunnel_rules WHERE published_snapshot IS NOT NULL ORDER BY id`)
	if err != nil {
		return err
	}
	var relays []published.Relay
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var relay published.Relay
		if err = json.Unmarshal([]byte(raw), &relay); err != nil {
			rows.Close()
			return err
		}
		if id != relay.ID {
			rows.Close()
			return fmt.Errorf("subscription relay %s: snapshot identity mismatch", id)
		}
		relays = append(relays, relay)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, relay := range relays {
		ids := relay.RouteNodeIDs
		if len(ids) == 0 {
			ids, err = r.legacyRelayNodes(ctx, tx, relay)
			if err != nil {
				return err
			}
		}
		if relay.InboundID != "" {
			if _, err = graph.Route(relay.InboundID); err != nil && !errors.Is(err, availability.ErrUnpublished) {
				return err
			}
		}
		for _, id := range ids {
			var exists int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM nodes WHERE id=?`, id).Scan(&exists); err != nil {
				return err
			}
			if exists != 1 {
				return fmt.Errorf("subscription relay %s: dependency node %s missing", relay.ID, id)
			}
		}
		if indexApplied == 0 {
			continue
		}
		record, e := availability.Read(ctx, tx, "relay:"+relay.ID)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return e
		}
		raw, e := json.Marshal(relay)
		if e != nil {
			return e
		}
		digest, e := availability.RelayDigest(raw)
		if e != nil {
			return e
		}
		if record.SourceDigest != digest || record.OwnerNodeID != relay.Entry.ID || record.TargetInboundID != relay.InboundID || !slices.Equal(availability.Unique(record.PathNodeIDs), availability.Unique(ids)) {
			return fmt.Errorf("subscription relay %s: index publication proof mismatch", relay.ID)
		}
	}
	return tx.Commit()
}

// RebuildSubscriptionDependencies upgrades the panel-only index from retained
// successful publication evidence. It never publishes or consults route drafts.
func (r *Repository) RebuildSubscriptionDependencies(ctx context.Context) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rebuildErr := r.rebuildSubscriptionDependenciesTx(ctx, tx)
	return errors.Join(rebuildErr, tx.Commit())
}

// Run after Deployment has merged the confirmed runtime and retained journals.
// The recovery orchestrator supplies the authenticated configuration vault.
func (r *Repository) PrepareSubscriptionDependenciesRestoreTx(ctx context.Context, tx *sql.Tx) error {
	return r.rebuildSubscriptionDependenciesTx(ctx, tx)
}

func (r *Repository) rebuildSubscriptionDependenciesTx(ctx context.Context, tx *sql.Tx) error {
	graph, err := availability.LoadGraph(ctx, tx, r.Vault)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM subscription_route_dependencies`); err != nil {
		return err
	}
	nodes := make([]string, 0, len(graph.States))
	for id := range graph.States {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)
	var failures []error
	for _, id := range nodes {
		if err = availability.CaptureNodeTx(ctx, tx, r.Vault, id); err != nil {
			failures = append(failures, fmt.Errorf("subscription node %s: %w", id, err))
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,published_snapshot FROM tunnel_rules WHERE published_snapshot IS NOT NULL ORDER BY id`)
	if err != nil {
		return err
	}
	type saved struct{ id, raw string }
	var relays []saved
	for rows.Next() {
		var item saved
		if err = rows.Scan(&item.id, &item.raw); err != nil {
			rows.Close()
			return err
		}
		relays = append(relays, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range relays {
		var relay published.Relay
		if err = json.Unmarshal([]byte(item.raw), &relay); err != nil {
			failures = append(failures, fmt.Errorf("subscription relay %s: %w", item.id, err))
			continue
		}
		if relay.ID != item.id {
			failures = append(failures, fmt.Errorf("subscription relay %s: snapshot identity mismatch", item.id))
			continue
		}
		// New snapshots carry the route captured before publication. The legacy
		// form needs a matching successful sealed journal to prove each hop.
		if len(relay.RouteNodeIDs) == 0 {
			relay.RouteNodeIDs, err = r.legacyRelayNodes(ctx, tx, relay)
			if err != nil {
				failures = append(failures, err)
				continue
			}
		}
		if err = r.saveRelaySnapshot(ctx, tx, relay); err != nil {
			failures = append(failures, fmt.Errorf("subscription relay %s: %w", item.id, err))
		}
	}
	return errors.Join(failures...)
}

func (r *Repository) legacyRelayNodes(ctx context.Context, tx *sql.Tx, relay published.Relay) ([]string, error) {
	wantRaw, err := json.Marshal(relay)
	if err != nil {
		return nil, err
	}
	want, err := availability.RelayDigest(wantRaw)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,tunnel_publication_json FROM deployment_batches
		WHERE status='success' AND tunnel_finalized=1 AND tunnel_publication_json IS NOT NULL ORDER BY created_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var batchID string
		var raw []byte
		if err = rows.Scan(&batchID, &raw); err != nil {
			return nil, err
		}
		publication, err := r.decodeTunnelPublication(raw)
		if err != nil {
			return nil, fmt.Errorf("subscription relay %s: successful journal %s: %w", relay.ID, batchID, err)
		}
		candidate, exists := publication.relays[relay.ID]
		if !exists {
			continue
		}
		encoded, err := json.Marshal(candidate)
		if err != nil {
			return nil, err
		}
		digest, err := availability.RelayDigest(encoded)
		if err != nil {
			return nil, err
		}
		if digest != want {
			continue
		}
		graph := &availability.Graph{States: map[string]desiredstate.State{}}
		var entryID string
		for _, captured := range publication.desired {
			state := graph.States[captured.input.NodeID]
			state.NodeID = captured.input.NodeID
			state.Inbounds = append(state.Inbounds, desiredstate.Inbound{ID: captured.id, Port: captured.input.Port,
				Enabled: captured.input.Enabled, Protocol: captured.input.Protocol, DefaultOutbound: captured.input.DefaultOutbound, Settings: captured.input.Settings})
			graph.States[state.NodeID] = state
			if captured.owner.Kind == "rule" && captured.owner.ID == relay.ID && captured.input.NodeID == relay.Entry.ID {
				entryID = captured.id
			}
		}
		if entryID == "" {
			return nil, fmt.Errorf("subscription relay %s: successful journal %s lacks captured rule listener", relay.ID, batchID)
		}
		ids, err := graph.Route(entryID)
		if err != nil {
			return nil, fmt.Errorf("subscription relay %s: successful journal %s: %w", relay.ID, batchID, err)
		}
		return ids, nil
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("subscription relay %s: cannot reconstruct published path; matching successful sealed journal and captured listeners missing", relay.ID)
}

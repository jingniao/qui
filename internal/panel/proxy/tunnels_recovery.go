package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/platform/vault"
)

// ValidateTunnels accepts pre-0129 archives and validates every persisted draft,
// sealed credential set and claim before recovery migrates or serves a backup.
func ValidateTunnels(ctx context.Context, database *sql.DB, key *vault.Vault) error {
	if err := validateTunnelMigrationSchema(ctx, database); err != nil {
		return err
	}
	var applied, present int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0129_tunnels.sql'").Scan(&applied); err != nil {
		return err
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('tunnels','tunnel_hops','tunnel_rules','port_claims','tunnel_objects')").Scan(&present); err != nil {
		return err
	}
	if present != applied*5 {
		return errors.New("tunnel schema mismatch")
	}
	if applied == 0 {
		return nil
	}
	r := &Repository{Database: database, Vault: key}
	tunnels, err := r.Tunnels(ctx)
	if err != nil {
		return err
	}
	for _, t := range tunnels {
		if !managedName(t.Name) || t.Transport != "tls" || len(t.Hops) < 2 || len(t.Hops) > 3 {
			return errors.New("invalid tunnel")
		}
		if _, err = r.openGostMaterial(t.ID, t.sealedMaterial); err != nil {
			return err
		}
		seen := map[string]bool{}
		for i, h := range t.Hops {
			role := "mid"
			if i == 0 {
				role = "entry"
			} else if i == len(t.Hops)-1 {
				role = "exit"
			}
			if h.Role != role || seen[h.NodeID] || !validPortMode(h.PortMode) || (h.ConnectHost != "" && !validForwardHost(h.ConnectHost)) {
				return errors.New("invalid tunnel hop")
			}
			seen[h.NodeID] = true
			if i > 0 {
				if err = validatePortClaim(ctx, database, h.NodeID, *h.ListenPort, "tunnel_hop", t.ID); err != nil {
					return err
				}
			}
		}
	}
	rules, err := r.TunnelRules(ctx, "", "")
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if !managedName(rule.Name) || !validPortMode(rule.PortMode) {
			return errors.New("invalid tunnel rule")
		}
		if rule.Target.Mode == "manual" && !validForwardHost(rule.Target.Host) {
			return errors.New("invalid tunnel rule target")
		}
		if rule.TunnelID != nil {
			var entry string
			if err = database.QueryRowContext(ctx, "SELECT node_id FROM tunnel_hops WHERE tunnel_id=? AND role='entry'", *rule.TunnelID).Scan(&entry); err != nil {
				return err
			}
			if entry != rule.EntryNodeID && rule.Status != "pending_delete" {
				return errors.New("invalid tunnel rule entry")
			}
		}
		if err = validatePortClaim(ctx, database, rule.EntryNodeID, *rule.ListenPort, "rule", rule.ID); err != nil {
			return err
		}
	}
	var invalid int
	if err = database.QueryRowContext(ctx, `SELECT count(*) FROM port_claims c LEFT JOIN tunnels t ON c.owner_kind='tunnel_hop' AND t.id=c.owner_id LEFT JOIN tunnel_rules r ON c.owner_kind='rule' AND r.id=c.owner_id LEFT JOIN canvas_tunnels a ON c.owner_kind='canvas' AND a.id=c.owner_id WHERE coalesce(t.id,r.id,a.id) IS NULL`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return errors.New("orphan port claim")
	}
	rows, err := database.QueryContext(ctx, "SELECT node_id FROM port_claims UNION SELECT node_id FROM inbounds")
	if err != nil {
		return err
	}
	nodes := []string{}
	for rows.Next() {
		var node string
		if err = rows.Scan(&node); err != nil {
			rows.Close()
			return err
		}
		nodes = append(nodes, node)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, node := range nodes {
		occupants, e := r.portOccupants(ctx, database, node, nil, "enabled")
		if e != nil {
			return e
		}
		for _, owners := range occupants {
			if len(owners) > 1 {
				return errors.New("conflicting port claims")
			}
		}
	}
	if err = database.QueryRowContext(ctx, `SELECT count(*) FROM tunnel_objects o LEFT JOIN inbounds i ON i.id=o.object_id LEFT JOIN tunnels t ON o.owner_kind='tunnel' AND t.id=o.owner_id LEFT JOIN tunnel_rules r ON o.owner_kind='rule' AND r.id=o.owner_id WHERE i.id IS NULL OR coalesce(t.id,r.id) IS NULL OR i.protocol<>'forward' OR i.subscription_visible<>0 OR NOT EXISTS(SELECT 1 FROM port_claims c WHERE c.node_id=i.node_id AND c.port=i.port AND c.owner_id=o.owner_id AND c.owner_kind=CASE o.owner_kind WHEN 'tunnel' THEN 'tunnel_hop' ELSE 'rule' END)`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return errors.New("invalid tunnel object ownership or claim")
	}
	if err = database.QueryRowContext(ctx, `SELECT count(*) FROM tunnel_objects o JOIN canvas_tunnel_objects c ON c.object_kind=o.object_kind AND c.object_id=o.object_id`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return errors.New("duplicate tunnel object ownership")
	}
	// Retained claims must describe a draft or a still running owned listener.
	if err = database.QueryRowContext(ctx, `SELECT count(*) FROM port_claims c WHERE NOT EXISTS(
	 SELECT 1 FROM tunnel_hops h WHERE c.owner_kind='tunnel_hop' AND h.tunnel_id=c.owner_id AND h.node_id=c.node_id AND h.listen_port=c.port
	 UNION ALL SELECT 1 FROM tunnel_rules r WHERE c.owner_kind='rule' AND r.id=c.owner_id AND r.entry_node_id=c.node_id AND r.listen_port=c.port
	 UNION ALL SELECT 1 FROM tunnel_objects o JOIN inbounds i ON i.id=o.object_id WHERE c.owner_kind=CASE o.owner_kind WHEN 'tunnel' THEN 'tunnel_hop' ELSE 'rule' END AND o.owner_id=c.owner_id AND i.node_id=c.node_id AND i.port=c.port
	 UNION ALL SELECT 1 FROM canvas_tunnel_objects o JOIN inbounds i ON o.object_kind='inbound' AND i.id=o.object_id WHERE c.owner_kind='canvas' AND o.tunnel_id=c.owner_id AND i.node_id=c.node_id AND i.port=c.port
	 UNION ALL SELECT 1 FROM tunnels t WHERE c.owner_kind='tunnel_hop' AND t.id=c.owner_id AND t.published_at IS NOT NULL AND t.status<>'published' AND NOT EXISTS(SELECT 1 FROM tunnel_objects o WHERE o.owner_kind='tunnel' AND o.owner_id=t.id)
	 UNION ALL SELECT 1 FROM tunnel_rules r WHERE c.owner_kind='rule' AND r.id=c.owner_id AND r.published_at IS NOT NULL AND r.status<>'published' AND NOT EXISTS(SELECT 1 FROM tunnel_objects o WHERE o.owner_kind='rule' AND o.owner_id=r.id)
	)`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return errors.New("port claim does not match owner listener")
	}
	var snapshots int
	if err = database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0130_relay_subscription_snapshot.sql'").Scan(&snapshots); err != nil {
		return err
	}
	if snapshots != 0 {
		rows, err := database.QueryContext(ctx, "SELECT id,published_snapshot FROM tunnel_rules WHERE published_snapshot IS NOT NULL")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, raw string
			if err = rows.Scan(&id, &raw); err != nil {
				return err
			}
			var snapshot published.Relay
			if err = json.Unmarshal([]byte(raw), &snapshot); err != nil || snapshot.ID != id || snapshot.Port < 1 || snapshot.Port > 65535 || (snapshot.Network != "tcp" && snapshot.Network != "udp" && snapshot.Network != "both") {
				return errors.New("invalid published tunnel snapshot")
			}
		}
		return rows.Err()
	}
	return nil
}

func validateTunnelMigrationSchema(ctx context.Context, database *sql.DB) error {
	var snapshotApplied, snapshotColumn int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0130_relay_subscription_snapshot.sql'").Scan(&snapshotApplied); err != nil {
		return err
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('tunnel_rules') WHERE name='published_snapshot'").Scan(&snapshotColumn); err != nil {
		return err
	}
	if snapshotApplied != snapshotColumn {
		return errors.New("tunnel publication snapshot schema mismatch")
	}
	var applied, present int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0131_canvas_forward_migration.sql'").Scan(&applied); err != nil {
		return err
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('canvas_forward_reconcile','tunnel_migration_report')").Scan(&present); err != nil {
		return err
	}
	if present != applied*2 {
		return errors.New("canvas forward migration schema mismatch")
	}
	if applied == 0 {
		return nil
	}
	if snapshotApplied == 0 {
		return errors.New("canvas forward migration requires tunnel snapshot schema")
	}
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM canvas_forward_reconcile WHERE id=1 AND done IN (0,1) AND json_valid(migrated_ids) AND json_type(migrated_ids)='array' AND (done=1 OR json_array_length(migrated_ids)=0)").Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("invalid canvas forward migration marker")
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM canvas_forward_reconcile,json_each(migrated_ids) WHERE type<>'text' OR length(value)=0").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("invalid migrated tunnel identity")
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM tunnel_migration_report WHERE length(tunnel_id)=0 OR length(trim(name))=0 OR length(trim(reason))=0").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("invalid tunnel migration report")
	}
	return nil
}
func validatePortClaim(ctx context.Context, database *sql.DB, node string, port int, kind, owner string) error {
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM port_claims WHERE node_id=? AND port=? AND owner_kind=? AND owner_id=?", node, port, kind, owner).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("missing port claim")
	}
	return nil
}

// PrepareTunnelsRestoreTx keeps archived allocations and sealed material. A
// restored publication is a draft requiring explicit publication on the nodes.
func (r *Repository) PrepareTunnelsRestoreTx(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"tunnels", "tunnel_rules"} {
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET status='modified',draft_version=draft_version+1,updated_at=? WHERE published_at IS NOT NULL AND status<>'pending_delete'", timestamp()); err != nil {
			return err
		}
	}
	return nil
}

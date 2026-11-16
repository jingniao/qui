package node

import (
	"context"
	"database/sql"
)

// PrepareGroupRestoreTx keeps today's group definitions and entry membership
// authoritative. Inbounds absent from the business snapshot cannot be restored;
// removed entries must never regain access through an older backup.
// Run after importer recovery, which replaces sources (and cascades references).
func (r *Repository) PrepareGroupRestoreTx(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		"DELETE FROM node_groups",
		"INSERT INTO node_groups SELECT * FROM restore_source.node_groups",
		"INSERT INTO node_group_servers SELECT e.* FROM restore_source.node_group_servers e JOIN nodes n ON n.id=e.node_id WHERE n.deleted_at IS NULL",
		`INSERT INTO node_group_inbounds SELECT e.* FROM restore_source.node_group_inbounds e
JOIN inbounds i ON i.id=e.inbound_id JOIN nodes n ON n.id=i.node_id WHERE n.deleted_at IS NULL`,
		"INSERT INTO node_group_imports SELECT e.* FROM restore_source.node_group_imports e JOIN subscription_sources s ON s.id=e.source_id",
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	// Tunnel rule members (0151) follow the same rule: today's membership, for
	// rules the restored data still holds with a linked target.
	var present int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.sqlite_master WHERE type='table' AND name='node_group_relays'").Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO node_group_relays(group_id,rule_id) SELECT e.group_id,e.rule_id FROM restore_source.node_group_relays e
JOIN node_groups g ON g.id=e.group_id JOIN tunnel_rules t ON t.id=e.rule_id WHERE t.target_mode='linked'`)
	return err
}

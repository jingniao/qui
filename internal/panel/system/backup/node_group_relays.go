package backup

import (
	"context"
	"database/sql"
)

// validateNodeGroupRelays checks tunnel rule members (0151): each names an
// existing group and an existing linked rule, within the per-group limit.
func validateNodeGroupRelays(ctx context.Context, database *sql.DB) error {
	var applied, present int
	if database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0151_node_group_relays.sql'").Scan(&applied) != nil ||
		database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='node_group_relays'").Scan(&present) != nil || present != applied {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	for _, q := range []string{
		"SELECT count(*) FROM node_group_relays e LEFT JOIN node_groups g ON g.id=e.group_id LEFT JOIN tunnel_rules t ON t.id=e.rule_id WHERE g.id IS NULL OR t.id IS NULL OR t.target_mode<>'linked'",
		"SELECT count(*) FROM (SELECT group_id FROM node_group_relays GROUP BY group_id HAVING count(*)>500)",
	} {
		var n int
		if database.QueryRowContext(ctx, q).Scan(&n) != nil || n != 0 {
			return ErrArchive
		}
	}
	return nil
}

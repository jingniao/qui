package backup

import (
	"context"
	"database/sql"
)

// validateTunnelImportTargets accepts pre-0152 archives and removed import
// targets, but rejects a target ID that no longer names an imported proxy.
func validateTunnelImportTargets(ctx context.Context, database *sql.DB) error {
	var applied, present int
	if database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0152_tunnel_rule_import_target.sql'").Scan(&applied) != nil ||
		database.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('tunnel_rules') WHERE name='target_import_id'").Scan(&present) != nil || present != applied {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	var invalid int
	if database.QueryRowContext(ctx, `SELECT count(*) FROM tunnel_rules t LEFT JOIN imported_proxies p ON p.id=t.target_import_id
WHERE (t.target_import_id IS NOT NULL AND (t.target_mode<>'import' OR p.id IS NULL))
OR (t.target_mode='import' AND (t.target_inbound_id IS NOT NULL OR t.target_host IS NOT NULL OR t.target_port IS NOT NULL))`).Scan(&invalid) != nil || invalid != 0 {
		return ErrArchive
	}
	return nil
}

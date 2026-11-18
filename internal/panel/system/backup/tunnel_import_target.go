package backup

import (
	"context"
	"database/sql"
)

// validateTunnelImportTargets accepts removed import targets but rejects a target ID that no longer names an imported proxy.
func validateTunnelImportTargets(ctx context.Context, database *sql.DB) error {
	var invalid int
	if database.QueryRowContext(ctx, `SELECT count(*) FROM tunnel_rules t LEFT JOIN imported_proxies p ON p.id=t.target_import_id
WHERE (t.target_import_id IS NOT NULL AND (t.target_mode<>'import' OR p.id IS NULL))
OR (t.target_mode='import' AND (t.target_inbound_id IS NOT NULL OR t.target_host IS NOT NULL OR t.target_port IS NOT NULL))`).Scan(&invalid) != nil || invalid != 0 {
		return ErrArchive
	}
	return nil
}

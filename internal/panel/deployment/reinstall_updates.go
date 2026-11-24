package deployment

import (
	"context"
	"database/sql"
)

// SupersedeNodeUpdates releases only the replaced identity's unfinished work.
// It runs in the identity recovery transaction, preserving original execution
// evidence rather than inventing a successful cancellation or installation.
func (r *Repository) SupersedeNodeUpdates(ctx context.Context, tx *sql.Tx, nodeID string) error {
	for _, query := range []string{
		"UPDATE agent_system_update_tasks SET status='superseded',updated_at=? WHERE node_id=? AND (status IN ('queued','running','unconfirmed') OR (status='failed' AND json_extract(result_json,'$.recovery.attempted')=1 AND json_extract(result_json,'$.recovery.success') IS NOT 1))",
		"UPDATE agent_update_tasks SET status='superseded',updated_at=? WHERE node_id=? AND status IN ('queued','running','failed_closed')",
		"UPDATE core_update_tasks SET status='superseded',updated_at=? WHERE node_id=? AND status IN ('queued','running','failed_closed')",
	} {
		if _, err := tx.ExecContext(ctx, query, now(), nodeID); err != nil {
			return err
		}
	}
	return nil
}

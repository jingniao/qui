package deployment

import (
	"context"
	"database/sql"
)

// CloseNodeTasks closes a deleted node's own single-node work inside the caller's
// transaction, so the node cannot leave a pending operation behind. Multi-node
// batches are never touched: an active batch blocks the deletion instead.
func (r *Repository) CloseNodeTasks(ctx context.Context, tx *sql.Tx, nodeID string) error {
	revisions, err := pendingTargetRevisions(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE deployment_targets SET status='failed',result_code='node_deleted',updated_at=? WHERE node_id=? AND status='pending'", now(), nodeID); err != nil {
		return err
	}
	// The single-node failure path marks the revision of a failed target; a
	// revision this node was the last pending target of ends the same way.
	for _, revision := range revisions {
		if _, err := tx.ExecContext(ctx, "UPDATE config_revisions SET status='failed' WHERE id=? AND status='pending' AND NOT EXISTS(SELECT 1 FROM deployment_targets WHERE revision=? AND status='pending')", revision, revision); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE deployment_commands SET status='failed',result_code='node_deleted' WHERE node_id=? AND status='pending'", nodeID); err != nil {
		return err
	}
	// Core updates have a cleared state: the Owner no longer tracks them.
	if _, err := tx.ExecContext(ctx, "UPDATE core_update_tasks SET status='cleared',updated_at=? WHERE node_id=? AND status IN ('queued','running','failed_closed')", now(), nodeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE core_remove_tasks SET status='unconfirmed',updated_at=? WHERE node_id=? AND status IN ('queued','running')", now(), nodeID); err != nil {
		return err
	}
	if err := closeAgentUpdateTasks(ctx, tx, nodeID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE node_health_checks SET status='expired' WHERE node_id=? AND status='queued'", nodeID)
	return err
}

func pendingTargetRevisions(ctx context.Context, tx *sql.Tx, nodeID string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT revision FROM deployment_targets WHERE node_id=? AND status='pending'", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	revisions := []int64{}
	for rows.Next() {
		var revision int64
		if err := rows.Scan(&revision); err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}

// An Agent update task has no cleared state, its terminal statuses must carry a
// result only the Agent can produce, and 'failed_closed' would stay counted as a
// pending operation that blocks restore. The active tasks of a deleted node are
// therefore removed with it; completed tasks stay as history.
func closeAgentUpdateTasks(ctx context.Context, tx *sql.Tx, nodeID string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM agent_update_attempts WHERE task_id IN (SELECT id FROM agent_update_tasks WHERE node_id=? AND status IN ('queued','running','failed_closed'))", nodeID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM agent_update_tasks WHERE node_id=? AND status IN ('queued','running','failed_closed')", nodeID)
	return err
}

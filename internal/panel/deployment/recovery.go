package deployment

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/db"
)

func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx) error {
	if err := requireDeploymentRestoreReceiptsTx(ctx, tx); err != nil {
		return err
	}
	for _, query := range []string{
		"SELECT count(*) FROM restore_source.agent_update_tasks WHERE status IN ('queued','running','failed_closed')",
		"SELECT count(*) FROM agent_update_tasks t LEFT JOIN restore_source.agent_update_tasks c ON c.id=t.id WHERE t.status IN ('queued','running','failed_closed') AND (c.id IS NULL OR c.status IN ('queued','running','failed_closed') OR c.node_id<>t.node_id OR c.original_command_id<>t.original_command_id OR c.baseline_revision<>t.baseline_revision)",
		"SELECT count(*) FROM restore_source.core_update_tasks WHERE status IN ('queued','running')",
		"SELECT count(*) FROM core_update_tasks t LEFT JOIN restore_source.core_update_tasks c ON c.id=t.id WHERE t.status IN ('queued','running') AND (c.id IS NULL OR c.status IN ('queued','running') OR c.node_id<>t.node_id OR c.baseline_revision<>t.baseline_revision OR c.sha256<>t.sha256)",
	} {
		if err := db.AssertRecoveryNone(ctx, tx, query); err != nil {
			return err
		}
	}
	for _, table := range []string{"agent_update_attempts", "agent_update_tasks", "core_update_tasks", "core_remove_tasks"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	for _, query := range []string{
		"SELECT count(*) FROM config_revisions s LEFT JOIN restore_source.config_revisions c ON c.id=s.id WHERE c.id IS NULL OR c.encrypted_state<>s.encrypted_state",
		"SELECT (SELECT count(*) FROM restore_source.deployment_targets WHERE status='pending')+(SELECT count(*) FROM restore_source.deployment_commands WHERE status='pending')+(SELECT count(*) FROM restore_source.deployment_batches WHERE status IN ('deploying','rolling_back','rollback_failed'))",
	} {
		if err := db.AssertRecoveryNone(ctx, tx, query); err != nil {
			return err
		}
	}
	// Only completed current coordination is reusable. Unknown in-flight
	// execution must be reconciled before preparation, never silently ACKed.
	for _, table := range []string{"node_runtime_state", "deployment_batch_items", "deployment_batches", "deployment_targets", "deployment_commands"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	if err := db.CopyRecoveryTable(ctx, tx, "config_revisions", "id,author,summary,status,created_at,encrypted_state,inbound_enabled_json"); err != nil {
		return err
	}
	if err := db.PreserveRecoverySequence(ctx, tx, "config_revisions"); err != nil {
		return err
	}
	for _, item := range []struct{ table, columns string }{
		{"deployment_targets", "revision,node_id,status,result_code,updated_at"},
		{"deployment_batches", "id,chain_id,name,author,status,created_at"},
		{"deployment_batch_items", "batch_id,position,node_id,revision,previous_revision,status,rollback_command_id"},
		{"deployment_commands", "id,node_id,revision,operation,status,result_code,created_at"},
		{"node_runtime_state", "node_id,revision"},
	} {
		if err := db.CopyRecoveryTable(ctx, tx, item.table, item.columns); err != nil {
			return err
		}
	}
	return nil
}

// Run after System has preserved the current signed catalogue. Snapshot jobs
// never issue old commands; only authenticated completed current tasks survive.
func (r *Repository) PrepareCoreUpdatesRestoreTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM core_remove_tasks"); err != nil {
		return err
	}
	if err := db.CopyRecoveryTable(ctx, tx, "core_remove_tasks", "id,node_id,component,revision,status,code,created_by,created_at,updated_at,result_json"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE core_remove_tasks SET status='unconfirmed',updated_at=? WHERE status IN ('queued','running')", now()); err != nil {
		return err
	}
	if err := db.CopyRecoveryTable(ctx, tx, "agent_update_tasks", agentTaskColumns); err != nil {
		return err
	}
	if err := db.CopyRecoveryTable(ctx, tx, "agent_update_attempts", "command_id,task_id,generation,operation,request_json,result_json,created_at"); err != nil {
		return err
	}
	if err := db.CopyRecoveryTable(ctx, tx, "core_update_tasks", "id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json,adding"); err != nil {
		return err
	}
	return nil
}

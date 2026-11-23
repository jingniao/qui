package access

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/db"
)

func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx) error {
	if err := validateTrafficStorageTx(ctx, tx, "restore_source."); err != nil {
		return err
	}
	// Bulk source replacement must not maintain a previous database's derived
	// totals. Rebuild from the final restored sources before enabling fast reads.
	if _, err := tx.ExecContext(ctx, "UPDATE traffic_sample_hours SET ready=0"); err != nil {
		return err
	}
	if err := r.preserveSpaceMaintenanceRestoreTx(ctx, tx); err != nil {
		return err
	}
	if err := r.PreservePendingTrafficRestoreTx(ctx, tx); err != nil {
		return err
	}
	if err := db.AssertRecoveryNone(ctx, tx, "SELECT count(*) FROM traffic_cursors s LEFT JOIN restore_source.traffic_cursors c ON c.node_id=s.node_id AND c.epoch=s.epoch AND c.credential_id=s.credential_id WHERE c.credential_id IS NULL OR c.user_id<>s.user_id OR c.device_id<>s.device_id OR c.revision<>s.revision OR c.upload_bytes<s.upload_bytes OR c.download_bytes<s.download_bytes"); err != nil {
		return err
	}
	for _, item := range []struct{ table, columns string }{
		{"node_traffic_cursors", "node_id,epoch,credential_id,core,revision,upload_bytes,download_bytes,observed_at"},
		{"core_traffic_samples", "node_id,core,epoch,observed_at,upload_bytes,download_bytes"},
		{"core_traffic_hourly", "node_id,core,bucket,upload_bytes,download_bytes,samples"},
		{"traffic_storage_job", "id,compacted_samples,retired_receipts,updated_at,last_success_at,error_code"},
		{"traffic_delivery_queues", "node_id,queue_id,completed_seq,confirmed_seq,retired_seq,candidate_seq,candidate_at,updated_at,closed_at,recovery_id,reset_id"},
		{"access_batches", "node_id,batch_id,digest,received_at,reset_id"}, {"traffic_batches", "node_id,batch_id,digest,received_at,accounting_completed_at,queue_id,delivery_seq,reset_id"},
		{"access_rollups", "id,bucket,node_id,user_id,device_id,domain,target_ip,allowed,dimensions_json,connections,upload_bytes,download_bytes,traffic_known"},
		{"traffic_hourly", "bucket,node_id,user_id,device_id,upload_bytes,download_bytes"}, {"traffic_daily", "bucket,node_id,user_id,device_id,upload_bytes,download_bytes"},
		{"traffic_cursors", "node_id,epoch,credential_id,upload_bytes,download_bytes,observed_at,user_id,device_id,revision"},
		{"account_traffic_hourly", "bucket,node_id,user_id,device_id,upload_bytes,download_bytes"}, {"account_traffic_daily", "bucket,node_id,user_id,device_id,upload_bytes,download_bytes"},
		{"account_traffic_samples", "node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes"},
	} {
		if err := db.CopyRecoveryTable(ctx, tx, item.table, item.columns); err != nil {
			return err
		}
	}
	var minuteTables int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.sqlite_master WHERE type='table' AND name IN ('core_traffic_minutes','account_traffic_minutes')").Scan(&minuteTables); err != nil {
		return err
	}
	if minuteTables == 1 {
		return ErrTrafficDelivery
	}
	if minuteTables == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM core_traffic_minutes; DELETE FROM account_traffic_minutes;"); err != nil {
			return err
		}
	}
	if minuteTables == 2 {
		for _, item := range []struct{ table, columns string }{
			{"core_traffic_minutes", "node_id,core,epoch,bucket,upload_bytes,download_bytes,samples"},
			{"account_traffic_minutes", "node_id,epoch,credential_id,user_id,device_id,bucket,upload_bytes,download_bytes,samples"},
		} {
			if err := db.CopyRecoveryTable(ctx, tx, item.table, item.columns); err != nil {
				return err
			}
		}
	}
	var resetTable int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.sqlite_master WHERE type='table' AND name='agent_state_resets'").Scan(&resetTable); err != nil {
		return err
	}
	if resetTable == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM agent_state_resets"); err != nil {
			return err
		}
	} else if err := db.CopyRecoveryTable(ctx, tx, "agent_state_resets", "reset_id,node_id,previous_fingerprint,public_key_digest,target_sha256,agent_version,certificate_pem,ca_pem,fingerprint,expires_at,sealed_at,discarded_usage"); err != nil {
		return err
	}
	if err := validateTrafficStorageTx(ctx, tx, ""); err != nil {
		return err
	}
	if err := resetTrafficHoursTx(ctx, tx); err != nil {
		return err
	}
	if err := r.preserveTrafficCompletionRestoreTx(ctx, tx); err != nil {
		return err
	}
	if _, err := scanRetentionJob(tx.QueryRowContext(ctx, "SELECT "+retentionJobColumns+" FROM restore_source.access_retention_job WHERE id=1")); err != nil {
		return err
	}
	return db.CopyRecoveryTable(ctx, tx, "access_retention_job", "id,"+retentionJobColumns)
}

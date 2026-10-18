package access

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/db"
)

func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx) error {
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
		{"access_batches", "node_id,batch_id,digest,received_at"}, {"traffic_batches", "node_id,batch_id,digest,received_at"},
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
	if err := r.preserveTrafficCompletionRestoreTx(ctx, tx); err != nil {
		return err
	}
	var present, applied int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.sqlite_master WHERE type='table' AND name='access_retention_job'").Scan(&present); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.schema_migrations WHERE version='0056_access_retention_job.sql'").Scan(&applied); err != nil {
		return err
	}
	if present == 0 && applied == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM access_retention_job"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO access_retention_job(id) VALUES(1)")
		return err
	}
	if present != 1 || applied != 1 {
		return ErrInvalid
	}
	if _, err := scanRetentionJob(tx.QueryRowContext(ctx, "SELECT "+retentionJobColumns+" FROM restore_source.access_retention_job WHERE id=1")); err != nil {
		return err
	}
	return db.CopyRecoveryTable(ctx, tx, "access_retention_job", "id,"+retentionJobColumns)
}

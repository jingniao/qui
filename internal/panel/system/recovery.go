package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"proxycontrol/internal/platform/db"
)

func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx, stamp, backupID string) error {
	// Retain current operator security/format choices across older backups.
	if _, err := tx.ExecContext(ctx, "DELETE FROM system_settings WHERE key='runtime_policy'"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) SELECT key,value_json FROM restore_source.system_settings WHERE key='runtime_policy'"); err != nil {
		return err
	}
	if _, err := readRuntimeSettings(ctx, tx, r.formats); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM release_catalogue"); err != nil {
		return err
	}
	if err := db.CopyRecoveryTable(ctx, tx, "release_catalogue", "id,component,operating_system,architecture,release_sequence,version,digest,signed_json,signing_public_key,created_by,created_at"); err != nil {
		return err
	}
	// Candidate logs must not drive the existing backup directory. Preserve
	// the current catalogue instead, and disable scheduling until reviewed.
	for _, query := range []string{"DELETE FROM backup_file_ops", "DELETE FROM backup_runs"} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	if err := db.CopyRecoveryTable(ctx, tx, "backups", "id,created_at,created_by,kind,format_version,file_bytes,sha256,verified_at,origin"); err != nil {
		return err
	}
	if err := db.CopyRecoveryTable(ctx, tx, "backup_runs", "id,kind,period_key,schedule_revision,status,attempts,backup_id,claim_token,lease_until,last_attempt_at,retry_at,error_code"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE backup_runs SET status='failed',claim_token='',lease_until='',retry_at='',error_code='restore_interrupted' WHERE status='running'"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM system_settings WHERE key='backup_schedule'"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) SELECT key,value_json FROM restore_source.system_settings WHERE key='backup_schedule'"); err != nil {
		return err
	}
	schedule, err := readBackupSchedule(ctx, tx)
	if err != nil || schedule.Revision == math.MaxInt64 {
		return ErrBackupScheduleInvalid
	}
	schedule.Policy.Enabled = false
	schedule.Revision++
	schedule.UpdatedAt, schedule.UpdatedBy = stamp, "local-restore"
	data, err := json.Marshal(schedule)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('backup_schedule',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", string(data)); err != nil {
		return err
	}
	if err := db.CopyRecoveryTable(ctx, tx, "admin_operations", "id,timestamp,operator,action,target,result,ip"); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,'local-restore','准备恢复数据库',?,'成功','local')", stamp, backupID)
	return err
}

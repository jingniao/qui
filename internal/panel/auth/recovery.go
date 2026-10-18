package auth

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/db"
)

// PrepareRestoreTx operates only on the offline candidate. Current password
// changes and disabled/deleted viewers must not be undone by an older backup.
func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx) error {
	for _, query := range []string{
		"SELECT abs((SELECT count(*) FROM admins WHERE role='Owner' AND status='active')-1)",
		"SELECT abs((SELECT count(*) FROM restore_source.admins WHERE role='Owner' AND status='active')-1)",
		"SELECT count(*) FROM admins a WHERE a.role='Owner' AND NOT EXISTS(SELECT 1 FROM restore_source.admins c WHERE c.id=a.id AND c.role='Owner' AND c.status='active')",
		"SELECT count(*) FROM admins a JOIN restore_source.admins c ON c.id=a.id WHERE a.role<>c.role",
	} {
		if err := db.AssertRecoveryNone(ctx, tx, query); err != nil {
			return err
		}
	}
	for _, query := range []string{
		"DELETE FROM sessions",
		"UPDATE admins SET status='disabled' WHERE role='Viewer' AND NOT EXISTS(SELECT 1 FROM restore_source.admins c WHERE c.id=admins.id)",
		"UPDATE admins SET password_hash=(SELECT c.password_hash FROM restore_source.admins c WHERE c.id=admins.id),status=CASE WHEN status='disabled' THEN 'disabled' ELSE (SELECT c.status FROM restore_source.admins c WHERE c.id=admins.id) END WHERE EXISTS(SELECT 1 FROM restore_source.admins c WHERE c.id=admins.id)",
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	if err := db.CopyRecoveryTable(ctx, tx, "login_attempts", "id,ip,attempted_at"); err != nil {
		return err
	}
	return nil
}

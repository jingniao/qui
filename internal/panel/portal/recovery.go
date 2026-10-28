package portal

import (
	"context"
	"database/sql"
)

// PrepareRestoreTx ends all portal sessions, including those in the backup.
// Portal owns the login columns, so their conservative merge stays here rather
// than teaching the user repository about portal authentication policy.
// The user restore step runs first and inserts new users with login disabled;
// merging their current password here must not enable those restored accounts.
func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx) error {
	for _, query := range []string{
		"DELETE FROM user_sessions",
		"UPDATE users SET login_enabled=0 WHERE NOT EXISTS(SELECT 1 FROM restore_source.users c WHERE c.id=users.id)",
		`UPDATE users SET password_hash=(SELECT c.password_hash FROM restore_source.users c WHERE c.id=users.id),
login_enabled=CASE WHEN login_enabled=1 AND (SELECT c.login_enabled FROM restore_source.users c WHERE c.id=users.id)=1 THEN 1 ELSE 0 END,
last_login_at=max(last_login_at,(SELECT c.last_login_at FROM restore_source.users c WHERE c.id=users.id))
WHERE EXISTS(SELECT 1 FROM restore_source.users c WHERE c.id=users.id)`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

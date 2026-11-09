package system

import (
	"context"
	"time"
)

// RecordDaily writes at most one audit row per operator, action and target per
// UTC day, and reports whether it wrote one. A repeating condition uses it (the
// certificate renewal that keeps failing, for instance) so the log and the alert
// stay one reminder a day instead of one per attempt, and so that "once a day"
// survives a panel restart without a new column.
func (r *Repository) RecordDaily(ctx context.Context, operator, action, target string) (bool, error) {
	now := time.Now().UTC()
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// The date prefix is the UTC date because every audit row stores a UTC
	// timestamp; comparing the full text would depend on its fractional part.
	var existing int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_operations WHERE operator=? AND action=? AND target=? AND substr(timestamp,1,10)=?",
		operator, action, target, now.Format("2006-01-02")).Scan(&existing); err != nil {
		return false, err
	}
	if existing > 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,?,?,'失败','')",
		now.Format(time.RFC3339Nano), operator, action, target); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

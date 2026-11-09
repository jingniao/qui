package certificate

import (
	"context"
	"database/sql"
	"time"
)

// PrepareRestoreTx turns the runs a snapshot caught mid-issuance into failed rows
// that the normal backoff retries. The archive's own rows are otherwise kept
// exactly as stored: a restore never starts an issuance by itself, and a
// certificate that was valid before the snapshot stays valid and renews on its
// own schedule.
func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx, stamp string) error {
	now, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,attempts FROM certificates WHERE status=?", StatusIssuing)
	if err != nil {
		return err
	}
	interrupted := []struct {
		id       string
		attempts int
	}{}
	for rows.Next() {
		var item struct {
			id       string
			attempts int
		}
		if err := rows.Scan(&item.id, &item.attempts); err != nil {
			rows.Close()
			return err
		}
		interrupted = append(interrupted, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range interrupted {
		attempts := item.attempts + 1
		if _, err := tx.ExecContext(ctx, "UPDATE certificates SET status=?,last_error_code=?,attempts=?,next_attempt_at=?,updated_at=? WHERE id=?",
			StatusFailed, CodeInterrupted, attempts, NextAttempt(now, attempts), stamp, item.id); err != nil {
			return err
		}
	}
	return nil
}

package spool

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// A panel that deterministically rejects a batch ends the control stream right
// after it was sent, so the Agent finds the same head batch again after every
// reconnect. After TrafficRejectLimit consecutive rejections of that batch it
// leaves the delivery head into quarantine: kept on disk, never deleted and
// never evicted for capacity (every traffic record outranks allowed access
// rollups), counted in the spool statistics, and skipped so later traffic and
// access batches flow again.
//
// Quarantine is a cool-down, not a verdict. The panel answers the same
// FailedPrecondition for transient trouble (a busy database), and a panel-side
// fix can make a previously rejected batch acceptable, so a quarantined batch is
// retried once nothing else is pending (see TrafficQuarantineCooldown) and a
// restart of the Agent releases every quarantined batch at once.
const TrafficRejectLimit = 3

// TrafficQuarantineCooldown is how long a quarantined batch waits behind the
// rest of the queue before it is re-attempted as the last resort. It is a
// variable so tests can shorten it.
var TrafficQuarantineCooldown = time.Hour

// RejectTraffic records one rejected delivery of a traffic batch and reports
// whether that rejection left the batch quarantined. A batch that is already
// acknowledged, evicted, or whose quarantine cool-down is still running is left
// untouched.
func (s *Spool) RejectTraffic(ctx context.Context, batchID string) (bool, error) {
	if batchID == "" || len(batchID) > 128 {
		return false, errors.New("invalid traffic rejection")
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var rejections, quarantined, quarantinedAt int64
	err = tx.QueryRowContext(ctx, "SELECT CASE WHEN typeof(rejections)='integer' THEN rejections ELSE -1 END,quarantined,CASE WHEN typeof(quarantined_at)='integer' THEN quarantined_at ELSE -1 END FROM traffic_batches WHERE id=?", batchID).Scan(&rejections, &quarantined, &quarantinedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if rejections < 0 || quarantinedAt < 0 || (quarantined != 0 && quarantined != 1) {
		return false, errors.New("traffic rejection counter invalid")
	}
	now := time.Now().Unix()
	if quarantined == 1 {
		// Only the retry the cool-down allowed counts. A report that arrives
		// while the cool-down still runs does not belong to a delivery and
		// leaves the row untouched, so repeated reports cannot extend it.
		if quarantinedAt > now-int64(TrafficQuarantineCooldown/time.Second) {
			return false, nil
		}
		result, err := tx.ExecContext(ctx, "UPDATE traffic_batches SET rejections=?,quarantined_at=? WHERE id=? AND quarantined=1 AND quarantined_at=?", TrafficRejectLimit-1, now, batchID, quarantinedAt)
		if err != nil {
			return false, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		if rows != 1 {
			return false, ErrQueuedBatchInvalid
		}
		return true, tx.Commit()
	}
	if rejections >= TrafficRejectLimit {
		return false, errors.New("traffic rejection counter invalid")
	}
	// The counter stops one short of the limit, so the rejection of the retry
	// that follows a cool-down quarantines the batch again at once.
	rejections++
	statement := "UPDATE traffic_batches SET rejections=? WHERE id=? AND quarantined=0 AND rejections=?"
	arguments := []any{rejections, batchID, rejections - 1}
	nowQuarantined := rejections >= TrafficRejectLimit
	if nowQuarantined {
		statement = "UPDATE traffic_batches SET rejections=?,quarantined=1,quarantined_at=? WHERE id=? AND quarantined=0 AND rejections=?"
		arguments = []any{rejections, now, batchID, rejections - 1}
	}
	result, err := tx.ExecContext(ctx, statement, arguments...)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows != 1 {
		return false, ErrQueuedBatchInvalid
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return nowQuarantined, nil
}

// attachTrafficQuarantine adds the quarantine columns to a store that was
// created before they existed. Their defaults keep every stored batch
// deliverable, and traffic_batches is outside the authenticated checkpoint, so
// the stored digest still verifies.
func attachTrafficQuarantine(ctx context.Context, database *sql.DB) error {
	present, err := trafficBatchColumns(ctx, database)
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"quarantined", "quarantined INTEGER NOT NULL DEFAULT 0 CHECK(quarantined IN (0,1))"},
		{"rejections", "rejections INTEGER NOT NULL DEFAULT 0 CHECK(rejections>=0)"},
		{"quarantined_at", "quarantined_at INTEGER NOT NULL DEFAULT 0 CHECK(quarantined_at>=0)"},
	} {
		if present[column.name] {
			continue
		}
		if _, err := database.ExecContext(ctx, "ALTER TABLE traffic_batches ADD COLUMN "+column.definition); err != nil {
			return err
		}
	}
	return nil
}

// releaseTrafficQuarantine puts every held-back batch back into the delivery
// head when the store opens. A restart or upgrade retries them immediately: the
// panel may have rejected them for a transient reason and a newer panel may
// accept them now, so a quarantine never outlives the process that set it.
func releaseTrafficQuarantine(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, "UPDATE traffic_batches SET quarantined=0,rejections=0,quarantined_at=0 WHERE quarantined=1")
	return err
}

func trafficBatchColumns(ctx context.Context, database *sql.DB) (map[string]bool, error) {
	rows, err := database.QueryContext(ctx, "PRAGMA table_info(traffic_batches)")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var index, notNull, primaryKey int
		var name, kind string
		var fallback sql.NullString
		if err := rows.Scan(&index, &name, &kind, &notNull, &fallback, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

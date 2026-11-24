package access

import (
	"context"
	"database/sql"
	"fmt"
	"math"
)

// PrepareNodeTrafficHours prepares a bounded number of existing buckets. Taking
// the write lock before selecting buckets serializes backfill with ingestion and
// compaction; triggers then maintain prepared rows in the source transaction.
func PrepareNodeTrafficHours(ctx context.Context, database *sql.DB, limit int) (bool, error) {
	if limit < 1 {
		return false, fmt.Errorf("traffic hour preparation limit must be positive")
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return false, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	rows, err := conn.QueryContext(ctx, `SELECT source,node_id,epoch,bucket FROM traffic_sample_hours WHERE ready=0 ORDER BY source,node_id,epoch,bucket LIMIT ?`, limit)
	if err != nil {
		return false, err
	}
	type key struct{ source, node, epoch, bucket string }
	var pending []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.source, &k.node, &k.epoch, &k.bucket); err != nil {
			rows.Close()
			return false, err
		}
		pending = append(pending, k)
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil {
		return false, readErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	for _, k := range pending {
		// source has a database CHECK constraint; only these two owned tables
		// participate in the display index.
		table := "core_traffic_samples"
		if k.source == "account" {
			table = "account_traffic_samples"
		}
		var up, down, samples int64
		overflow := false
		minuteTable := "core_traffic_minutes"
		if k.source == "account" {
			minuteTable = "account_traffic_minutes"
		}
		sourceRows, err := conn.QueryContext(ctx, `SELECT upload_bytes,download_bytes,1 AS samples FROM `+table+` WHERE node_id=? AND epoch=? AND substr(observed_at,1,13)=?
 UNION ALL SELECT upload_bytes,download_bytes,samples FROM `+minuteTable+` WHERE node_id=? AND epoch=? AND substr(bucket,1,13)=?`, k.node, k.epoch, k.bucket[:13], k.node, k.epoch, k.bucket[:13])
		if err != nil {
			return false, err
		}
		for sourceRows.Next() {
			var u, d, count int64
			if err := sourceRows.Scan(&u, &d, &count); err != nil {
				sourceRows.Close()
				return false, err
			}
			if u < 0 || d < 0 {
				sourceRows.Close()
				return false, fmt.Errorf("negative traffic source sample")
			}
			if overflow || u > math.MaxInt64-up || d > math.MaxInt64-down || count > math.MaxInt64-samples {
				overflow = true
			} else {
				up += u
				down += d
				samples += count
			}
		}
		readErr, closeErr := sourceRows.Err(), sourceRows.Close()
		if readErr != nil {
			return false, readErr
		}
		if closeErr != nil {
			return false, closeErr
		}
		if overflow {
			up, down, samples = 0, 0, 0
		}
		if samples == 0 && !overflow {
			if _, err := conn.ExecContext(ctx, `DELETE FROM traffic_sample_hours WHERE source=? AND node_id=? AND epoch=? AND bucket=?`, k.source, k.node, k.epoch, k.bucket); err != nil {
				return false, err
			}
			continue
		}
		// Overflow belongs to the display query, not to source ingestion. Mark
		// it explicitly so an excluded account epoch cannot block the ledger.
		if _, err := conn.ExecContext(ctx, `UPDATE traffic_sample_hours SET upload_bytes=?,download_bytes=?,samples=?,overflow=?,ready=1 WHERE source=? AND node_id=? AND epoch=? AND bucket=?`, up, down, samples, overflow, k.source, k.node, k.epoch, k.bucket); err != nil {
			return false, err
		}
	}
	var more bool
	if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM traffic_sample_hours WHERE ready=0)`).Scan(&more); err != nil {
		return false, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return false, err
	}
	return more, nil
}

// resetTrafficHoursTx is used after restoring authoritative tables. Derived
// readiness is never imported from an archive or carried across a restore.
func resetTrafficHoursTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM traffic_sample_hours;
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 SELECT DISTINCT 'core',node_id,epoch,substr(observed_at,1,13)||':00:00Z',0 FROM core_traffic_samples;
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 SELECT DISTINCT 'account',node_id,epoch,substr(observed_at,1,13)||':00:00Z',0 FROM account_traffic_samples;`)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 SELECT DISTINCT 'core',node_id,epoch,substr(bucket,1,13)||':00:00Z',0 FROM core_traffic_minutes;
 UPDATE traffic_sample_hours SET ready=0 WHERE source='core' AND EXISTS(SELECT 1 FROM core_traffic_minutes m WHERE m.node_id=traffic_sample_hours.node_id AND m.epoch=traffic_sample_hours.epoch AND substr(m.bucket,1,13)||':00:00Z'=traffic_sample_hours.bucket);
 INSERT OR IGNORE INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 SELECT DISTINCT 'account',node_id,epoch,substr(bucket,1,13)||':00:00Z',0 FROM account_traffic_minutes;
 UPDATE traffic_sample_hours SET ready=0 WHERE source='account' AND EXISTS(SELECT 1 FROM account_traffic_minutes m WHERE m.node_id=traffic_sample_hours.node_id AND m.epoch=traffic_sample_hours.epoch AND substr(m.bucket,1,13)||':00:00Z'=traffic_sample_hours.bucket);`)
	return err
}

package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

const trafficMinuteBatch = 500

type coreMinuteKey struct{ node, core, epoch, bucket string }
type accountMinuteKey struct{ node, epoch, credential, user, device, bucket string }

func minuteBucket(value string) (string, time.Time, error) {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", time.Time{}, err
	}
	start := at.UTC().Truncate(time.Minute)
	return timestamp(start), start, nil
}

func addMinuteCounter(total, value int64) (int64, error) {
	if value < 0 || total > math.MaxInt64-value {
		return 0, fmt.Errorf("traffic minute counter overflow")
	}
	return total + value, nil
}

func (r *Repository) compactTrafficMinutes(ctx context.Context, now time.Time) (bool, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	cutoff := now.UTC().Add(-24 * time.Hour).Truncate(time.Minute)
	var before int64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM core_traffic_samples WHERE observed_at<?)+(SELECT count(*) FROM account_traffic_samples WHERE observed_at<?)`, timestamp(cutoff), timestamp(cutoff)).Scan(&before); err != nil {
		return false, err
	}
	coreMore, err := compactCoreMinutesTx(ctx, tx, cutoff)
	if err != nil {
		return false, err
	}
	accountMore, err := compactAccountMinutesTx(ctx, tx, cutoff)
	if err != nil {
		return false, err
	}
	var after int64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM core_traffic_samples WHERE observed_at<?)+(SELECT count(*) FROM account_traffic_samples WHERE observed_at<?)`, timestamp(cutoff), timestamp(cutoff)).Scan(&after); err != nil {
		return false, err
	}
	if after > before {
		return false, fmt.Errorf("traffic minute compaction increased raw sample count")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE traffic_storage_job SET compacted_samples=compacted_samples+?,updated_at=? WHERE id=1", before-after, timestamp(now)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return coreMore || accountMore, nil
}

func compactCoreMinutesTx(ctx context.Context, tx *sql.Tx, cutoff time.Time) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT node_id,core,epoch,substr(observed_at,1,16)||':00.000000000Z' AS bucket
 FROM core_traffic_samples WHERE observed_at<? GROUP BY node_id,core,epoch,bucket ORDER BY bucket,node_id,core,epoch LIMIT ?`, timestamp(cutoff), trafficMinuteBatch)
	if err != nil {
		return false, err
	}
	var keys []coreMinuteKey
	for rows.Next() {
		var key coreMinuteKey
		if err := rows.Scan(&key.node, &key.core, &key.epoch, &key.bucket); err != nil {
			rows.Close()
			return false, err
		}
		keys = append(keys, key)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return false, err
	}
	for _, key := range keys {
		_, start, err := minuteBucket(key.bucket)
		if err != nil {
			return false, err
		}
		end := timestamp(start.Add(time.Minute))
		var up, down, samples int64
		if err := sumCoreMinute(ctx, tx, key, end, &up, &down, &samples); err != nil {
			return false, err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO core_traffic_minutes(node_id,core,epoch,bucket,upload_bytes,download_bytes,samples) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(node_id,core,epoch,bucket) DO UPDATE SET upload_bytes=core_traffic_minutes.upload_bytes+excluded.upload_bytes,download_bytes=core_traffic_minutes.download_bytes+excluded.download_bytes,samples=core_traffic_minutes.samples+excluded.samples
WHERE core_traffic_minutes.upload_bytes<=? - excluded.upload_bytes AND core_traffic_minutes.download_bytes<=? - excluded.download_bytes AND core_traffic_minutes.samples<=? - excluded.samples`, key.node, key.core, key.epoch, key.bucket, up, down, samples, math.MaxInt64, math.MaxInt64, math.MaxInt64)
		if err != nil {
			return false, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return false, err
		} else if n != 1 {
			return false, fmt.Errorf("traffic minute counter overflow")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM core_traffic_samples WHERE node_id=? AND core=? AND epoch=? AND observed_at>=? AND observed_at<?`, key.node, key.core, key.epoch, key.bucket, end); err != nil {
			return false, err
		}
	}
	return len(keys) == trafficMinuteBatch, nil
}

func sumCoreMinute(ctx context.Context, tx *sql.Tx, key coreMinuteKey, end string, up, down, samples *int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT upload_bytes,download_bytes FROM core_traffic_samples WHERE node_id=? AND core=? AND epoch=? AND observed_at>=? AND observed_at<?`, key.node, key.core, key.epoch, key.bucket, end)
	if err != nil {
		return err
	}
	for rows.Next() {
		var u, d int64
		if err := rows.Scan(&u, &d); err != nil {
			rows.Close()
			return err
		}
		if *up, err = addMinuteCounter(*up, u); err != nil {
			rows.Close()
			return err
		}
		if *down, err = addMinuteCounter(*down, d); err != nil {
			rows.Close()
			return err
		}
		if *samples == math.MaxInt64 {
			rows.Close()
			return fmt.Errorf("traffic minute counter overflow")
		}
		*samples++
	}
	return errors.Join(rows.Err(), rows.Close())
}

func compactAccountMinutesTx(ctx context.Context, tx *sql.Tx, cutoff time.Time) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT node_id,epoch,credential_id,user_id,device_id,substr(observed_at,1,16)||':00.000000000Z' AS bucket
 FROM account_traffic_samples WHERE observed_at<? GROUP BY node_id,epoch,credential_id,user_id,device_id,bucket ORDER BY bucket,node_id,epoch,credential_id LIMIT ?`, timestamp(cutoff), trafficMinuteBatch)
	if err != nil {
		return false, err
	}
	var keys []accountMinuteKey
	for rows.Next() {
		var key accountMinuteKey
		if err := rows.Scan(&key.node, &key.epoch, &key.credential, &key.user, &key.device, &key.bucket); err != nil {
			rows.Close()
			return false, err
		}
		keys = append(keys, key)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return false, err
	}
	for _, key := range keys {
		_, start, err := minuteBucket(key.bucket)
		if err != nil {
			return false, err
		}
		end := timestamp(start.Add(time.Minute))
		var up, down, samples int64
		rows, err := tx.QueryContext(ctx, `SELECT upload_bytes,download_bytes FROM account_traffic_samples WHERE node_id=? AND epoch=? AND credential_id=? AND user_id=? AND device_id=? AND observed_at>=? AND observed_at<?`, key.node, key.epoch, key.credential, key.user, key.device, key.bucket, end)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var u, d int64
			if err := rows.Scan(&u, &d); err != nil {
				rows.Close()
				return false, err
			}
			if up, err = addMinuteCounter(up, u); err != nil {
				rows.Close()
				return false, err
			}
			if down, err = addMinuteCounter(down, d); err != nil {
				rows.Close()
				return false, err
			}
			if samples == math.MaxInt64 {
				rows.Close()
				return false, fmt.Errorf("traffic minute counter overflow")
			}
			samples++
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return false, err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO account_traffic_minutes(node_id,epoch,credential_id,user_id,device_id,bucket,upload_bytes,download_bytes,samples) VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(node_id,epoch,credential_id,user_id,device_id,bucket) DO UPDATE SET upload_bytes=account_traffic_minutes.upload_bytes+excluded.upload_bytes,download_bytes=account_traffic_minutes.download_bytes+excluded.download_bytes,samples=account_traffic_minutes.samples+excluded.samples
WHERE account_traffic_minutes.upload_bytes<=? - excluded.upload_bytes AND account_traffic_minutes.download_bytes<=? - excluded.download_bytes AND account_traffic_minutes.samples<=? - excluded.samples`, key.node, key.epoch, key.credential, key.user, key.device, key.bucket, up, down, samples, math.MaxInt64, math.MaxInt64, math.MaxInt64)
		if err != nil {
			return false, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return false, err
		} else if n != 1 {
			return false, fmt.Errorf("traffic minute counter overflow")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM account_traffic_samples WHERE node_id=? AND epoch=? AND credential_id=? AND user_id=? AND device_id=? AND observed_at>=? AND observed_at<?`, key.node, key.epoch, key.credential, key.user, key.device, key.bucket, end); err != nil {
			return false, err
		}
	}
	return len(keys) == trafficMinuteBatch, nil
}

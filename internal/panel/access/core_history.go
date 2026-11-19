package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"proxycontrol/internal/platform/db"
	"time"
)

var ErrTrafficResolution = errors.New("traffic_history_resolution: 历史流量已按 UTC 小时合并，请选择 UTC 整点作为查询边界")

type NodeTrafficTotal struct {
	NodeID   string
	Bucket   string
	Upload   int64
	Download int64
	Samples  int64
}

// ReadNodeTraffic holds one read snapshot for both precision validation and sums.
// Account fallback is still decided by node_traffic_cursors in the existing view.
func ReadNodeTraffic(ctx context.Context, database *sql.DB, from, to time.Time, nodeID string, hourly bool) ([]NodeTrafficTotal, error) {
	tx, err := db.BeginRead(ctx, database)
	if err != nil {
		return nil, err
	}
	defer tx.Close()
	for _, boundary := range []time.Time{from, to} {
		if boundary.Equal(boundary.Truncate(time.Hour)) {
			continue
		}
		var count int
		err = tx.QueryRowContext(ctx, "SELECT count(*) FROM core_traffic_hourly WHERE bucket=? AND (?='' OR node_id=?)", timestamp(boundary.Truncate(time.Hour)), nodeID, nodeID).Scan(&count)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, fmt.Errorf("%w；冲突边界 %s，可选整点 %s 或 %s", ErrTrafficResolution, boundary.UTC().Format(time.RFC3339Nano), timestamp(boundary.Truncate(time.Hour)), timestamp(boundary.Truncate(time.Hour).Add(time.Hour)))
		}
	}
	group := "node_id"
	bucket := "''"
	if hourly {
		group = "node_id,substr(observed_at,1,13)"
		bucket = "substr(observed_at,1,13)||':00:00Z'"
	}
	// Indexed coarse bounds retain the boundary seconds; normalize only that range.
	coarseFrom := from.UTC().Format("2006-01-02T15:04:05")
	coarseTo := to.UTC()
	if coarseTo.Nanosecond() != 0 {
		coarseTo = coarseTo.Truncate(time.Second).Add(time.Second)
	}
	coarseEnd := coarseTo.Format("2006-01-02T15:04:05")
	args := []any{coarseFrom, coarseEnd, nodeID, nodeID, coarseFrom, coarseEnd, nodeID, nodeID}
	// The indexed bounds already select whole seconds exactly. Only fractional
	// boundary seconds need normalization of published seconds/nanoseconds. CASE
	// keeps that work out of the rest of the month, including inside the view.
	const normalized = "(substr(observed_at,1,19)||'.'||substr((CASE WHEN substr(observed_at,20,1)='.' THEN substr(observed_at,21,length(observed_at)-21) ELSE '' END)||'000000000',1,9)||'Z')"
	precise := ""
	if from.Nanosecond() != 0 {
		precise += " AND CASE WHEN observed_at<? THEN " + normalized + ">=? ELSE 1 END"
		args = append(args, from.UTC().Truncate(time.Second).Add(time.Second).Format("2006-01-02T15:04:05"), from.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	}
	if to.Nanosecond() != 0 {
		precise += " AND CASE WHEN observed_at>=? THEN " + normalized + "<? ELSE 1 END"
		args = append(args, to.UTC().Format("2006-01-02T15:04:05"), to.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	}
	rows, err := tx.QueryContext(ctx, `WITH traffic AS (
 SELECT node_id,observed_at,upload_bytes,download_bytes,1 AS samples FROM node_traffic_samples WHERE observed_at>=? AND observed_at<? AND (?='' OR node_id=?)
 UNION ALL SELECT node_id,bucket,upload_bytes,download_bytes,samples FROM core_traffic_hourly WHERE bucket>=? AND bucket<? AND (?='' OR node_id=?)
 ) SELECT node_id,`+bucket+`,sum(upload_bytes),sum(download_bytes),sum(samples) FROM traffic
	 WHERE 1=1`+precise+` GROUP BY `+group+` ORDER BY `+group, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NodeTrafficTotal{}
	var upload, download, samples int64
	for rows.Next() {
		var item NodeTrafficTotal
		if err := rows.Scan(&item.NodeID, &item.Bucket, &item.Upload, &item.Download, &item.Samples); err != nil {
			return nil, err
		}
		if item.Upload > math.MaxInt64-upload || item.Download > math.MaxInt64-download || item.Samples > math.MaxInt64-samples {
			return nil, fmt.Errorf("node traffic total overflow")
		}
		upload += item.Upload
		download += item.Download
		samples += item.Samples
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) compactCoreTraffic(ctx context.Context, now time.Time) (bool, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT rowid,node_id,core,substr(observed_at,1,13)||':00:00Z',upload_bytes,download_bytes FROM core_traffic_samples WHERE observed_at<? ORDER BY observed_at LIMIT 500`, now.Add(-7*24*time.Hour).UTC().Truncate(time.Hour).Format("2006-01-02T15:04:05"))
	if err != nil {
		return false, err
	}
	type sample struct {
		row                int64
		node, core, bucket string
		up, down           int64
	}
	items := []sample{}
	for rows.Next() {
		var s sample
		if err := rows.Scan(&s.row, &s.node, &s.core, &s.bucket, &s.up, &s.down); err != nil {
			rows.Close()
			return false, err
		}
		items = append(items, s)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return false, err
	}
	for _, s := range items {
		res, err := tx.ExecContext(ctx, `INSERT INTO core_traffic_hourly(node_id,core,bucket,upload_bytes,download_bytes,samples) VALUES(?,?,?,?,?,1)
 ON CONFLICT(node_id,core,bucket) DO UPDATE SET upload_bytes=upload_bytes+excluded.upload_bytes,download_bytes=download_bytes+excluded.download_bytes,samples=samples+1
 WHERE upload_bytes<=?-excluded.upload_bytes AND download_bytes<=?-excluded.download_bytes AND samples<?`, s.node, s.core, s.bucket, s.up, s.down, math.MaxInt64, math.MaxInt64, math.MaxInt64)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		if n != 1 {
			return false, fmt.Errorf("core traffic hourly counter overflow")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM core_traffic_samples WHERE rowid=?", s.row); err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE traffic_storage_job SET compacted_samples=compacted_samples+?,updated_at=? WHERE id=1", len(items), timestamp(now)); err != nil {
		return false, err
	}
	return len(items) == 500, tx.Commit()
}

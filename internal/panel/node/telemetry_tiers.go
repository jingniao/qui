package node

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const telemetryBucket = 5 * time.Minute
const telemetryBucketsPerPass = 64

var telemetryMetricColumns = strings.Split(metricColumns+",tcp_retrans_percent", ",")

// A rollup carries the final counter sample so that deleting its raw minutes
// does not destroy the next minute's valid retransmission-rate predecessor.
func metricSamplesCTE() string {
	nulls := strings.TrimSuffix(strings.Repeat("NULL,", len(strings.Split(metricColumns, ","))), ",")
	return `WITH input AS (
 SELECT at,` + sampleInsertColumns + `,1 AS raw FROM node_metric_samples WHERE node_id=? AND at>=? AND at<?
 UNION ALL SELECT tail_at,` + nulls + `,tail_out,tail_retrans,0 FROM node_metric_rollups WHERE node_id=? AND tail_at>=? AND tail_at<?
), ordered AS (
 SELECT *,CAST(strftime('%s',substr(at,1,19)||'Z') AS INTEGER) AS ts,
 LAG(CAST(strftime('%s',substr(at,1,19)||'Z') AS INTEGER)) OVER(ORDER BY at) AS prev_ts,
 LAG(tcp_out_segs) OVER(ORDER BY at) AS prev_out,
 LAG(tcp_retrans_segs) OVER(ORDER BY at) AS prev_retrans FROM input
), samples AS (
 SELECT *,CASE WHEN prev_out IS NOT NULL AND prev_retrans IS NOT NULL
 AND tcp_out_segs IS NOT NULL AND tcp_retrans_segs IS NOT NULL AND ts-prev_ts=60
 AND tcp_out_segs>prev_out AND tcp_retrans_segs>=prev_retrans
 THEN CAST(tcp_retrans_segs-prev_retrans AS REAL)*100.0/CAST(tcp_out_segs-prev_out AS REAL) END AS tcp_retrans_percent
 FROM ordered WHERE raw=1 AND at>=? AND at<?
)`
}

type telemetryKey struct{ node, target, bucket string }

// CompactTelemetry advances a bounded number of complete five-minute buckets.
// Accounting never reads these display tables. The merge and raw deletion share
// one write transaction, so an interruption cannot count a minute twice.
func (r *Repository) CompactTelemetry(ctx context.Context, now time.Time) (bool, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	expires := timestamp(now.UTC().Add(-7 * 24 * time.Hour))
	changed := false
	for _, table := range []string{"node_metric_samples", "node_latency_samples", "node_metric_rollups", "node_latency_rollups"} {
		column := "at"
		cutoff := expires
		if strings.HasSuffix(table, "rollups") {
			column = "bucket"
			cutoff = timestamp(now.UTC().Add(-7 * 24 * time.Hour).Truncate(telemetryBucket))
		}
		result, e := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE rowid IN (SELECT rowid FROM `+table+` WHERE `+column+`<? LIMIT 500)`, cutoff)
		if e != nil {
			return false, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return false, e
		}
		changed = changed || n > 0
	}
	cutoff := timestamp(now.UTC().Add(-24 * time.Hour).Truncate(telemetryBucket))
	for _, kind := range []string{"metric", "latency"} {
		target := "''"
		subTarget := "'' AS target_id"
		if kind == "latency" {
			target = "target_id"
			subTarget = "target_id"
		}
		query := `SELECT node_id,` + target + `,strftime('%Y-%m-%dT%H:%M:%S',CAST(strftime('%s',substr(at,1,19)||'Z') AS INTEGER)/300*300,'unixepoch')||'.000000000Z' AS bucket FROM (SELECT node_id,` + subTarget + `,at FROM node_` + kind + `_samples WHERE at<? ORDER BY at LIMIT ?) GROUP BY node_id,` + target + `,bucket ORDER BY bucket,node_id LIMIT ?`
		// A constant expression must not be interpreted as a GROUP BY ordinal.
		if kind == "metric" {
			query = strings.Replace(query, "GROUP BY node_id,'',bucket", "GROUP BY node_id,bucket", 1)
		}
		rows, e := tx.QueryContext(ctx, query, cutoff, telemetryBucketsPerPass*8, telemetryBucketsPerPass)
		if e != nil {
			return false, e
		}
		keys := []telemetryKey{}
		for rows.Next() {
			var k telemetryKey
			if e = rows.Scan(&k.node, &k.target, &k.bucket); e != nil {
				rows.Close()
				return false, e
			}
			keys = append(keys, k)
		}
		if e = errors.Join(rows.Err(), rows.Close()); e != nil {
			return false, e
		}
		for _, k := range keys {
			if kind == "metric" {
				e = compactMetricBucket(ctx, tx, k)
			} else {
				e = compactLatencyBucket(ctx, tx, k)
			}
			if e != nil {
				return false, e
			}
		}
		changed = changed || len(keys) > 0
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return changed, nil
}

func compactMetricBucket(ctx context.Context, tx *sql.Tx, k telemetryKey) error {
	start, err := time.Parse(time.RFC3339Nano, k.bucket)
	if err != nil {
		return err
	}
	end := timestamp(start.Add(telemetryBucket))
	lookback := timestamp(start.Add(-time.Minute))
	cols := []string{"node_id", "bucket", "samples"}
	values := []string{"?", "?", "count(*)"}
	updates := []string{"samples=samples+excluded.samples"}
	for _, c := range telemetryMetricColumns {
		for _, op := range []string{"sum", "count", "min", "max"} {
			name := c + "_" + op
			cols = append(cols, name)
			values = append(values, op+"("+c+")")
			switch op {
			case "sum":
				updates = append(updates, name+"=CASE WHEN "+c+"_count=0 THEN excluded."+name+" WHEN excluded."+c+"_count=0 THEN "+name+" ELSE "+name+"+excluded."+name+" END")
			case "count":
				updates = append(updates, name+"="+name+"+excluded."+name)
			default:
				updates = append(updates, name+"=CASE WHEN "+name+" IS NULL THEN excluded."+name+" WHEN excluded."+name+" IS NULL THEN "+name+" ELSE "+op+"("+name+",excluded."+name+") END")
			}
		}
	}
	cols = append(cols, "tail_at", "tail_out", "tail_retrans")
	values = append(values, "?", "?", "?")
	updates = append(updates, "tail_at=excluded.tail_at", "tail_out=excluded.tail_out", "tail_retrans=excluded.tail_retrans")
	var tail string
	var out, retrans *int64
	if err = tx.QueryRowContext(ctx, `SELECT at,tcp_out_segs,tcp_retrans_segs FROM node_metric_samples WHERE node_id=? AND at>=? AND at<? ORDER BY at DESC LIMIT 1`, k.node, k.bucket, end).Scan(&tail, &out, &retrans); err != nil {
		return err
	}
	query := metricSamplesCTE() + ` INSERT INTO node_metric_rollups(` + strings.Join(cols, ",") + `) SELECT ` + strings.Join(values, ",") + ` FROM samples HAVING count(*)>0 ON CONFLICT(node_id,bucket) DO UPDATE SET ` + strings.Join(updates, ",")
	if _, err = tx.ExecContext(ctx, query, k.node, lookback, end, k.node, lookback, end, k.bucket, end, k.node, k.bucket, tail, out, retrans); err != nil {
		return fmt.Errorf("compact metric bucket: %w", err)
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM node_metric_samples WHERE node_id=? AND at>=? AND at<?`, k.node, k.bucket, end)
	return err
}

func compactLatencyBucket(ctx context.Context, tx *sql.Tx, k telemetryKey) error {
	start, err := time.Parse(time.RFC3339Nano, k.bucket)
	if err != nil {
		return err
	}
	end := timestamp(start.Add(telemetryBucket))
	_, err = tx.ExecContext(ctx, `INSERT INTO node_latency_rollups(node_id,target_id,bucket,samples,sent,failed,median_sum,median_count,median_min,median_max)
 SELECT node_id,target_id,?,count(*),sum(sent),sum(failed),sum(CASE WHEN sent>failed THEN median_ms END),count(CASE WHEN sent>failed THEN median_ms END),min(CASE WHEN sent>failed THEN median_ms END),max(CASE WHEN sent>failed THEN median_ms END)
 FROM node_latency_samples WHERE node_id=? AND target_id=? AND at>=? AND at<? GROUP BY node_id,target_id
 ON CONFLICT(node_id,target_id,bucket) DO UPDATE SET samples=samples+excluded.samples,sent=sent+excluded.sent,failed=failed+excluded.failed,
 median_sum=CASE WHEN median_count=0 THEN excluded.median_sum WHEN excluded.median_count=0 THEN median_sum ELSE median_sum+excluded.median_sum END,
 median_count=median_count+excluded.median_count,
 median_min=CASE WHEN median_min IS NULL THEN excluded.median_min WHEN excluded.median_min IS NULL THEN median_min ELSE min(median_min,excluded.median_min) END,
 median_max=CASE WHEN median_max IS NULL THEN excluded.median_max WHEN excluded.median_max IS NULL THEN median_max ELSE max(median_max,excluded.median_max) END`, k.bucket, k.node, k.target, k.bucket, end)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM node_latency_samples WHERE node_id=? AND target_id=? AND at>=? AND at<?`, k.node, k.target, k.bucket, end)
	return err
}

func telemetryStepSeconds(hours int, coarse bool) int {
	step := latencyStepSeconds(hours)
	if coarse {
		step = (step + 299) / 300 * 300
	}
	return step
}

func metricTierQuery(node string, since time.Time, step int) (string, []any) {
	raw, coarse, avgs := []string{}, []string{}, []string{}
	for _, c := range telemetryMetricColumns {
		raw = append(raw, c+" AS "+c+"_sum", "CASE WHEN "+c+" IS NULL THEN 0 ELSE 1 END AS "+c+"_count")
		coarse = append(coarse, c+"_sum", c+"_count")
		avgs = append(avgs, "CAST(SUM("+c+"_sum) AS REAL)/SUM("+c+"_count)")
	}
	until := timestamp(time.Now().UTC().Add(telemetryBucket))
	query := metricSamplesCTE() + `, combined AS (
 SELECT at,` + strings.Join(raw, ",") + `,1 AS samples FROM samples
	 UNION ALL SELECT bucket,` + strings.Join(coarse, ",") + `,samples FROM node_metric_rollups WHERE node_id=? AND bucket>=? AND bucket<?
), bucketed AS (SELECT *,CAST(strftime('%s',substr(at,1,19)||'Z') AS INTEGER)/?*? AS bucket FROM combined)
 SELECT strftime('%Y-%m-%dT%H:%M:%S',bucket,'unixepoch')||'.000000000Z',` + strings.Join(avgs, ",") + `,SUM(samples) FROM bucketed GROUP BY bucket ORDER BY bucket`
	lookback := timestamp(since.Add(-time.Minute))
	from := timestamp(since)
	return query, []any{node, lookback, until, node, lookback, until, from, until, node, from, until, step, step}
}

func latencyTierQuery(node, target string, since time.Time, step int) (string, []any) {
	query := `WITH combined AS (
 SELECT at,1 AS samples,sent,failed,CASE WHEN sent>failed THEN median_ms END AS median_sum,
 CASE WHEN sent>failed AND median_ms IS NOT NULL THEN 1 ELSE 0 END AS median_count,
 CASE WHEN sent>failed THEN median_ms END AS median_min,CASE WHEN sent>failed THEN median_ms END AS median_max
 FROM node_latency_samples WHERE node_id=? AND target_id=? AND at>=?
 UNION ALL SELECT bucket,samples,sent,failed,median_sum,median_count,median_min,median_max FROM node_latency_rollups WHERE node_id=? AND target_id=? AND bucket>=?
), bucketed AS (SELECT *,CAST(strftime('%s',substr(at,1,19)||'Z') AS INTEGER)/?*? AS bucket FROM combined)
 SELECT strftime('%Y-%m-%dT%H:%M:%S',bucket,'unixepoch')||'.000000000Z',NULL,
 MIN(median_min),MAX(median_max),SUM(median_sum)/SUM(median_count),SUM(sent),SUM(failed),SUM(samples)
 FROM bucketed GROUP BY bucket ORDER BY bucket`
	return query, []any{node, target, timestamp(since), node, target, timestamp(since), step, step}
}

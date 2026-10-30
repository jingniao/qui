package backup

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// Strip telemetry from the isolated copy, never the running database. VACUUM
// also removes deleted samples from free pages before the archive is hashed.
func stripLatencySnapshot(ctx context.Context, path string) error {
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	var present int
	if err = database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='node_latency_samples'").Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		return nil
	}
	for _, q := range []string{"PRAGMA secure_delete=ON", "DELETE FROM node_latency_samples", "DELETE FROM node_tcp_probes WHERE source='scheduled'", "VACUUM"} {
		if _, err = database.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func validateLatency(ctx context.Context, database *sql.DB) error {
	var applied, present int
	if database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0120_latency_samples.sql'").Scan(&applied) != nil || database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='node_latency_samples'").Scan(&present) != nil || applied != present {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	var count int
	if database.QueryRowContext(ctx, "SELECT count(*) FROM probe_targets WHERE scheduled NOT IN (0,1)").Scan(&count) != nil || count != 0 {
		return ErrArchive
	}
	if database.QueryRowContext(ctx, "SELECT count(*) FROM probe_targets WHERE scheduled=1").Scan(&count) != nil || count > 5 {
		return ErrArchive
	}
	if database.QueryRowContext(ctx, "SELECT count(*) FROM node_tcp_probes WHERE source NOT IN ('manual','scheduled')").Scan(&count) != nil || count != 0 {
		return ErrArchive
	}
	rows, err := database.QueryContext(ctx, "SELECT dispatched_at FROM node_tcp_probes")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var at *string
		if rows.Scan(&at) != nil {
			rows.Close()
			return ErrArchive
		}
		if at != nil {
			if _, err := time.Parse(time.RFC3339Nano, *at); err != nil {
				rows.Close()
				return ErrArchive
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	if database.QueryRowContext(ctx, "SELECT count(*) FROM (SELECT node_id FROM node_tcp_probes WHERE status='queued' AND dispatched_at IS NOT NULL GROUP BY node_id HAVING count(*)>1)").Scan(&count) != nil || count != 0 {
		return ErrArchive
	}
	// Reading all columns catches missing material even for an empty table.
	rows, err = database.QueryContext(ctx, "SELECT node_id,target_id,at,avg_ms,min_ms,max_ms,sent,failed,median_ms FROM node_latency_samples LIMIT 0")
	if err != nil {
		return ErrArchive
	}
	rows.Close()
	return nil
}

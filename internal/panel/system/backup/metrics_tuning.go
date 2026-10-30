package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

func validateMetricsTuning(ctx context.Context, db *sql.DB) error {
	var applied, present int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0123_node_metrics_tuning.sql'").Scan(&applied) != nil || db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('node_metric_samples','node_tuning')").Scan(&present) != nil || present != applied*2 {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	rows, err := db.QueryContext(ctx, "SELECT node_id,at,cpu_percent,memory_percent,disk_percent,memory_used_bytes,swap_used_bytes,disk_used_bytes,load1,process_count,tcp_connections,udp_sockets,active_connections,rx_bytes_per_second,tx_bytes_per_second FROM node_metric_samples LIMIT 0")
	if err != nil {
		return ErrArchive
	}
	rows.Close()
	rows, err = db.QueryContext(ctx, "SELECT desired_json,request_id,status,result_json,requested_at,updated_at FROM node_tuning")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var raw, status string
		var id, result, requested, updated *string
		if rows.Scan(&raw, &id, &status, &result, &requested, &updated) != nil {
			return ErrArchive
		}
		var settings map[string]string
		if json.Unmarshal([]byte(raw), &settings) != nil || settings == nil || node.ValidateTuning(settings, false) != nil {
			return ErrArchive
		}
		if status != "pending" && status != "applied" && status != "cleared" && status != "failed" && status != "unconfirmed" {
			return ErrArchive
		}
		if id == nil || *id == "" || len(*id) > 128 {
			return ErrArchive
		}
		for _, stamp := range []*string{requested, updated} {
			if stamp == nil {
				return ErrArchive
			}
			if _, err = time.Parse(time.RFC3339Nano, *stamp); err != nil {
				return ErrArchive
			}
		}
		if result != nil {
			var r pb.TuningResult
			if protojson.Unmarshal([]byte(*result), &r) != nil || node.ValidateTuningResult(&r) != nil || r.RequestId != *id || status != r.Status {
				return ErrArchive
			}
		} else if status != "pending" && status != "unconfirmed" {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}

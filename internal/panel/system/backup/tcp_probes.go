package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/tcpprobe"
	"time"
)

func validateTCPProbes(ctx context.Context, db *sql.DB) error {
	var applied int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0119_tcp_probes.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	for _, table := range []string{"probe_targets", "node_tcp_probes"} {
		var present int
		if db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&present) != nil || present != applied {
			return ErrArchive
		}
	}
	if applied == 0 {
		return nil
	}
	rows, err := db.QueryContext(ctx, "SELECT id,label,host,port,allow_non_public FROM probe_targets")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var t node.ProbeTarget
		if rows.Scan(&t.ID, &t.Label, &t.Host, &t.Port, &t.AllowNonPublic) != nil || t.ID == "" || t.Validate() != nil {
			rows.Close()
			return ErrArchive
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	rows, err = db.QueryContext(ctx, "SELECT id,command_json,result_json,created_at,deadline,status FROM node_tcp_probes")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var id, command, created, deadline, status string
		var result *string
		if rows.Scan(&id, &command, &result, &created, &deadline, &status) != nil {
			return ErrArchive
		}
		at, e := time.Parse(time.RFC3339Nano, created)
		limit, e2 := time.Parse(time.RFC3339Nano, deadline)
		var c pb.TCPProbeCommand
		if e != nil || e2 != nil || json.Unmarshal([]byte(command), &c) != nil || c.Id != id || tcpprobe.Validate(&c, at) != nil || !limit.Equal(c.Deadline.AsTime()) || (status != "queued" && status != "complete" && status != "expired") {
			return ErrArchive
		}
		if result != nil {
			var r pb.TCPProbeResult
			if json.Unmarshal([]byte(*result), &r) != nil || tcpprobe.ValidateResult(&c, &r) != nil {
				return ErrArchive
			}
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	var latencyApplied int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0120_latency_samples.sql'").Scan(&latencyApplied) != nil {
		return ErrArchive
	}
	filter := ""
	if latencyApplied != 0 {
		filter = " WHERE source='manual'"
	}
	var count int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM (SELECT count(*) AS n FROM node_tcp_probes"+filter+" GROUP BY node_id,target_id HAVING n>100)").Scan(&count) != nil || count != 0 {
		return ErrArchive
	}
	return nil
}

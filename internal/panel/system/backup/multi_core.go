package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func validateMultiCore(ctx context.Context, db *sql.DB) error {
	var applied, present int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0125_multi_core_panel.sql'").Scan(&applied) != nil || db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('core_remove_tasks','node_traffic_cursors','core_traffic_samples')").Scan(&present) != nil || present != applied*3 {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	for _, query := range []string{
		"SELECT count(*) FROM inbounds WHERE core NOT IN ('','xray','sing-box')",
		"SELECT count(*) FROM node_traffic_cursors WHERE credential_id<>'node' OR core NOT IN ('xray','sing-box') OR epoch NOT LIKE '%.node' OR revision<1 OR typeof(upload_bytes)<>'integer' OR typeof(download_bytes)<>'integer' OR upload_bytes<0 OR download_bytes<0 OR upload_bytes>1125899906842624 OR download_bytes>1125899906842624",
		"SELECT count(*) FROM core_traffic_samples WHERE core NOT IN ('xray','sing-box') OR typeof(upload_bytes)<>'integer' OR typeof(download_bytes)<>'integer' OR upload_bytes<0 OR download_bytes<0",
		"SELECT count(*) FROM core_remove_tasks WHERE component NOT IN ('xray','sing-box') OR revision<1 OR status NOT IN ('queued','running','success','rejected','unconfirmed')",
		"SELECT count(*) FROM core_remove_tasks r JOIN core_update_tasks u ON u.node_id=r.node_id AND u.component=r.component WHERE r.status IN ('queued','running') AND u.status IN ('queued','running')",
	} {
		var bad int
		if db.QueryRowContext(ctx, query).Scan(&bad) != nil || bad != 0 {
			return ErrArchive
		}
	}
	rows, err := db.QueryContext(ctx, "SELECT id,component,revision,status,code,result_json,created_at,updated_at FROM core_remove_tasks")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var id, component, status, code, created, updated string
		var revision int64
		var raw []byte
		if rows.Scan(&id, &component, &revision, &status, &code, &raw, &created, &updated) != nil {
			return ErrArchive
		}
		if _, err := time.Parse(time.RFC3339Nano, created); err != nil {
			return ErrArchive
		}
		if _, err := time.Parse(time.RFC3339Nano, updated); err != nil {
			return ErrArchive
		}
		if len(raw) > 0 {
			r := &pb.RemoveCoreResult{}
			if json.Unmarshal(raw, r) != nil || r.Id != id || r.Component != component || r.Revision != revision || r.Code != code || r.Success != (status == "success") || r.Success != (code == "success") {
				return ErrArchive
			}
		} else if status == "success" || status == "rejected" {
			return ErrArchive
		}
	}
	return rows.Err()
}

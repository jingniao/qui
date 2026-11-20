package backup

import (
	"context"
	"database/sql"
	"encoding/json"

	"proxycontrol/pkg/contracts/release"
)

func validateAgentSystemUpdates(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0157_agent_system_updates.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	} // Published pre-1.7.5 archives are migrated on restore.
	for _, query := range []string{
		"SELECT count(*) FROM node_update_capabilities WHERE agent_system_supported NOT IN (0,1) OR agent_system_error IS NULL OR agent_bootstrap_version IS NULL",
		"SELECT count(*) FROM agent_system_update_tasks t WHERE generation<>(SELECT count(*) FROM agent_system_update_attempts a WHERE a.task_id=t.id) OR NOT EXISTS(SELECT 1 FROM agent_system_update_attempts a WHERE a.attempt_id=t.attempt_id AND a.task_id=t.id AND a.generation=t.generation)",
		"SELECT count(*) FROM agent_system_update_tasks t JOIN agent_system_update_attempts a ON a.attempt_id=t.attempt_id WHERE (t.result_json IS NULL)<>(a.result_json IS NULL) OR t.result_json<>a.result_json",
		"SELECT count(*) FROM agent_system_update_tasks t JOIN agent_system_update_attempts a ON a.attempt_id=t.attempt_id WHERE t.dispatch_error<>coalesce(json_extract(a.dispatch_error_json,'$.error'),'')",
		"SELECT count(*) FROM agent_system_update_tasks t JOIN core_update_tasks c ON c.node_id=t.node_id WHERE t.status IN ('queued','running','unconfirmed') AND c.status IN ('queued','running','failed_closed')",
		"SELECT count(*) FROM agent_system_update_tasks t JOIN agent_update_tasks c ON c.node_id=t.node_id WHERE t.status IN ('queued','running','unconfirmed') AND c.status IN ('queued','running','failed_closed')",
	} {
		var invalid int
		if err := database.QueryRowContext(ctx, query).Scan(&invalid); err != nil || invalid != 0 {
			return ErrArchive
		}
	}
	rows, err := database.QueryContext(ctx, "SELECT a.attempt_id,t.id,t.node_id,t.baseline_revision,t.target_version,a.request_json,a.result_json,t.status,t.attempt_id,a.log_sequence,a.log_offset,a.progress_json,a.dispatch_error_json FROM agent_system_update_attempts a JOIN agent_system_update_tasks t ON t.id=a.task_id")
	if err != nil {
		return ErrArchive
	}
	type attempt struct {
		id       string
		sequence uint64
		offset   int64
	}
	attempts := []attempt{}
	for rows.Next() {
		var id, task, node, target, status, current string
		var revision, offset int64
		var sequence uint64
		var requestData, resultData, progressData, dispatchData []byte
		if rows.Scan(&id, &task, &node, &revision, &target, &requestData, &resultData, &status, &current, &sequence, &offset, &progressData, &dispatchData) != nil {
			rows.Close()
			return ErrArchive
		}
		q, err := release.DecodeAgentSystemUpdateRequest(requestData)
		if err != nil || q.AttemptID != id || q.TaskID != task || q.NodeID != node || q.Revision != revision || q.TargetVersion != target {
			rows.Close()
			return ErrArchive
		}
		if len(resultData) > 0 {
			r, err := release.DecodeAgentSystemUpdateResult(resultData)
			if err != nil || r.AttemptID != id || r.TaskID != task || r.NodeID != node || r.Revision != revision || r.TargetVersion != target || id == current && r.Status != status {
				rows.Close()
				return ErrArchive
			}
		} else if id == current && status != "queued" && status != "running" && status != "unconfirmed" {
			rows.Close()
			return ErrArchive
		}
		if len(progressData) > 0 {
			var p release.AgentSystemUpdateProgress
			if json.Unmarshal(progressData, &p) != nil || p.AttemptID != id || p.Sequence == 0 || p.Phase == "" {
				rows.Close()
				return ErrArchive
			}
		}
		if len(dispatchData) > 0 {
			p, err := release.DecodeAgentSystemUpdateProgress(dispatchData)
			if err != nil || p.AttemptID != id || p.Sequence != 0 || p.Phase != "request_submit" {
				rows.Close()
				return ErrArchive
			}
		}
		attempts = append(attempts, attempt{id, sequence, offset})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	for _, a := range attempts {
		logs, err := database.QueryContext(ctx, "SELECT sequence,offset,sha256,content FROM agent_system_update_logs WHERE attempt_id=? ORDER BY sequence", a.id)
		if err != nil {
			return ErrArchive
		}
		var sequence uint64
		var offset int64
		for logs.Next() {
			var c release.AgentSystemUpdateLogChunk
			c.AttemptID = a.id
			if logs.Scan(&c.Sequence, &c.Offset, &c.SHA256, &c.Content) != nil || c.Validate() != nil || c.Sequence != sequence+1 || c.Offset != offset {
				logs.Close()
				return ErrArchive
			}
			sequence = c.Sequence
			offset += int64(len(c.Content))
		}
		err = logs.Err()
		logs.Close()
		if err != nil || sequence != a.sequence || offset != a.offset {
			return ErrArchive
		}
	}
	return nil
}

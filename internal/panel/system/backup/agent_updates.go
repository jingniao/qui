package backup

import (
	"context"
	"database/sql"
	"proxycontrol/pkg/contracts/release"
)

func validateAgentUpdates(ctx context.Context, database *sql.DB) error {
	var invalid int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM node_update_capabilities WHERE agent_recovery_supported IS NULL OR agent_recovery_supported NOT IN (0,1) OR (agent_recovery_supported=1 AND agent_supported<>1)").Scan(&invalid); err != nil || invalid != 0 {
		return ErrArchive
	}
	releases, err := catalogueReleases(ctx, database)
	if err != nil {
		return ErrArchive
	}
	var bad int
	for _, query := range []string{
		`SELECT count(*) FROM node_update_capabilities WHERE agent_supported IS NULL OR agent_supported NOT IN (0,1)`,
		`SELECT count(*) FROM agent_update_tasks a JOIN core_update_tasks c ON c.node_id=a.node_id WHERE a.status IN ('queued','running','failed_closed') AND c.status IN ('queued','running','failed_closed')`,
		`SELECT count(*) FROM agent_update_tasks t JOIN agent_update_attempts a ON a.command_id=t.command_id WHERE (t.result_json IS NULL)<>(a.result_json IS NULL) OR t.result_json<>a.result_json`,
		`SELECT count(*) FROM agent_update_tasks t WHERE NOT EXISTS(SELECT 1 FROM deployment_targets d WHERE d.node_id=t.node_id AND d.revision=t.baseline_revision AND d.status='success')`,
	} {
		if err := database.QueryRowContext(ctx, query).Scan(&bad); err != nil || bad != 0 {
			return ErrArchive
		}
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM agent_update_tasks t WHERE generation<>(SELECT count(*) FROM agent_update_attempts a WHERE a.task_id=t.id) OR NOT EXISTS(SELECT 1 FROM agent_update_attempts a WHERE a.command_id=t.command_id AND a.task_id=t.id AND a.generation=t.generation) OR NOT EXISTS(SELECT 1 FROM agent_update_attempts a WHERE a.command_id=t.original_command_id AND a.task_id=t.id AND a.operation='install' AND a.generation=1)").Scan(&bad); err != nil || bad != 0 {
		return ErrArchive
	}
	rows, err := database.QueryContext(ctx, "SELECT t.node_id,t.baseline_revision,t.release_id,a.command_id,a.operation,a.request_json,a.result_json,t.status,t.command_id,t.original_command_id,a.generation,t.generation FROM agent_update_attempts a JOIN agent_update_tasks t ON t.id=a.task_id")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID, entryID, commandID, operation, status, current, original string
		var revision, generation, maxGeneration int64
		var requestData, resultData []byte
		if rows.Scan(&nodeID, &revision, &entryID, &commandID, &operation, &requestData, &resultData, &status, &current, &original, &generation, &maxGeneration) != nil {
			return ErrArchive
		}
		request, err := release.DecodeAgentUpdateRequest(requestData)
		entry, ok := releases[entryID]
		if err != nil || !ok || request.NodeID != nodeID || request.ID != commandID || request.Revision != revision || request.Operation != operation || request.Release.Manifest != entry.Manifest {
			return ErrArchive
		}
		if generation < 1 || generation > maxGeneration || operation == "install" && (generation != 1 || commandID != original) || operation == "recover" && (generation == 1 || request.OriginalID != original) {
			return ErrArchive
		}
		if len(resultData) > 0 {
			result, err := release.DecodeUpdateResult(resultData)
			if err != nil || release.ValidateAgentResult(request, result) != nil || commandID == current && result.Status != status {
				return ErrArchive
			}
		} else if commandID == current && status != "queued" && status != "running" {
			return ErrArchive
		}
	}
	return rows.Err()
}

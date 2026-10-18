package backup

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"proxycontrol/pkg/contracts/release"
	"time"
)

func validateAgentUpdates(ctx context.Context, database *sql.DB) error {
	var present int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('agent_update_tasks','agent_update_attempts')").Scan(&present); err != nil {
		return ErrArchive
	}
	if present == 0 {
		var applied int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version IN ('0036_agent_updates.sql','0037_agent_recovery.sql')").Scan(&applied); err != nil || applied != 0 {
			return ErrArchive
		}
		return nil
	}
	if present != 2 {
		return ErrArchive
	}
	var recoveryColumn, recoveryMigration int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('node_update_capabilities') WHERE name='agent_recovery_supported'").Scan(&recoveryColumn); err != nil {
		return ErrArchive
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0037_agent_recovery.sql'").Scan(&recoveryMigration); err != nil {
		return ErrArchive
	}
	if recoveryMigration != 0 && recoveryColumn != 1 {
		return ErrArchive
	}
	if recoveryColumn == 1 {
		var invalid int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM node_update_capabilities WHERE agent_recovery_supported IS NULL OR agent_recovery_supported NOT IN (0,1) OR (agent_recovery_supported=1 AND agent_supported<>1)").Scan(&invalid); err != nil || invalid != 0 {
			return ErrArchive
		}
	}
	rows, err := database.QueryContext(ctx, "SELECT id,component,operating_system,architecture,release_sequence,version,digest,signed_json,signing_public_key,created_at FROM release_catalogue")
	if err != nil {
		return ErrArchive
	}
	releases := map[string]release.Signed{}
	for rows.Next() {
		var id, component, platform, arch, version, digest, pin, created string
		var sequence int64
		var data []byte
		if rows.Scan(&id, &component, &platform, &arch, &sequence, &version, &digest, &data, &pin, &created) != nil {
			rows.Close()
			return ErrArchive
		}
		signed, err := release.Decode(data)
		key, keyErr := base64.StdEncoding.DecodeString(pin)
		at, timeErr := time.Parse(time.RFC3339Nano, created)
		if err != nil || keyErr != nil || len(key) != ed25519.PublicKeySize || timeErr != nil || signed.Manifest.Component != component || signed.Manifest.OS != platform || signed.Manifest.Architecture != arch || signed.Manifest.Sequence != sequence || signed.Manifest.Version != version || signed.Manifest.SHA256 != digest || signed.Verify(release.Policy{Component: component, OS: platform, Architecture: arch, Now: at, Keys: map[string]release.TrustedKey{signed.KeyID: {PublicKey: key, Components: []string{component}}}}) != nil {
			rows.Close()
			return ErrArchive
		}
		releases[id] = signed
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ErrArchive
	}
	rows.Close()
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
	rows, err = database.QueryContext(ctx, "SELECT t.node_id,t.baseline_revision,t.release_id,a.command_id,a.operation,a.request_json,a.result_json,t.status,t.command_id,t.original_command_id,a.generation,t.generation FROM agent_update_attempts a JOIN agent_update_tasks t ON t.id=a.task_id")
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

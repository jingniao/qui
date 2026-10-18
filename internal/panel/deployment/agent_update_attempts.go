package deployment

import (
	"context"
	"database/sql"
	"proxycontrol/pkg/contracts/release"
)

type AgentUpdateAttempt struct {
	CommandID  string                `json:"commandId"`
	Generation int64                 `json:"generation"`
	Operation  string                `json:"operation"`
	Current    bool                  `json:"current"`
	Status     string                `json:"status"`
	CreatedAt  string                `json:"createdAt"`
	Result     *release.UpdateResult `json:"result,omitempty"`
}

func (r *Repository) ListAgentUpdateAttempts(ctx context.Context, taskID string, page, size int) ([]AgentUpdateAttempt, int, error) {
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var current, status, nodeID string
	var revision int64
	if err := tx.QueryRowContext(ctx, "SELECT command_id,status,node_id,baseline_revision FROM agent_update_tasks WHERE id=?", taskID).Scan(&current, &status, &nodeID, &revision); err != nil {
		return nil, 0, err
	}
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM agent_update_attempts WHERE task_id=?", taskID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT command_id,generation,operation,request_json,result_json,created_at FROM agent_update_attempts WHERE task_id=? ORDER BY generation DESC LIMIT ? OFFSET ?", taskID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []AgentUpdateAttempt{}
	for rows.Next() {
		var item AgentUpdateAttempt
		var requestData, resultData []byte
		if err := rows.Scan(&item.CommandID, &item.Generation, &item.Operation, &requestData, &resultData, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		request, err := release.DecodeAgentUpdateRequest(requestData)
		if err != nil || request.ID != item.CommandID || request.NodeID != nodeID || request.Revision != revision || request.Operation != item.Operation {
			return nil, 0, release.ErrUpdate
		}
		item.Current = item.CommandID == current
		item.Status = "unconfirmed"
		if item.Current {
			item.Status = status
		}
		if len(resultData) > 0 {
			result, err := release.DecodeUpdateResult(resultData)
			if err != nil || release.ValidateAgentResult(request, result) != nil || item.Current && result.Status != status {
				return nil, 0, release.ErrUpdate
			}
			item.Result = &result
			item.Status = result.Status
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

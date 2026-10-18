package deployment

import (
	"context"
	"encoding/json"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/release"
)

type AgentRecoveryConfirmation struct {
	NodeID      string `json:"confirmNodeId"`
	Revision    int64  `json:"revision"`
	CommandID   string `json:"commandId"`
	Generation  int64  `json:"generation"`
	Unconfirmed bool   `json:"unconfirmed,omitempty"`
}

func (r *Repository) RetryAgentRecovery(ctx context.Context, catalogue ReleaseAccess, taskID, actor, ip string, confirm AgentRecoveryConfirmation) (AgentUpdateTask, error) {
	if actor == "" || len(actor) > 128 || confirm.Generation < 1 {
		return AgentUpdateTask{}, release.ErrUpdate
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return AgentUpdateTask{}, err
	}
	defer tx.Rollback()
	task, err := scanAgentTask(tx.QueryRowContext(ctx, "SELECT "+agentTaskColumns+" FROM agent_update_tasks WHERE id=?", taskID))
	if err != nil {
		return task, err
	}
	if task.Generation != confirm.Generation || task.NodeID != confirm.NodeID || task.Revision != confirm.Revision || task.CommandID != confirm.CommandID {
		return task, ErrNodeBusy
	}
	if confirm.Unconfirmed {
		if task.Status != "running" || task.Result != nil {
			return task, ErrNodeBusy
		}
	} else if task.Status != "failed_closed" || task.Result == nil {
		return task, ErrNodeBusy
	}
	var enabled, supported bool
	var revision int64
	if err := tx.QueryRowContext(ctx, "SELECT n.enabled,c.agent_recovery_supported AND c.agent_supported,s.revision FROM nodes n JOIN node_update_capabilities c ON c.node_id=n.id JOIN node_runtime_state s ON s.node_id=n.id WHERE n.id=?", task.NodeID).Scan(&enabled, &supported, &revision); err != nil {
		return task, err
	}
	if !enabled || !supported || revision != task.Revision {
		return task, ErrNodeBusy
	}
	var requestData, resultData []byte
	if err := tx.QueryRowContext(ctx, "SELECT request_json,result_json FROM agent_update_attempts WHERE command_id=? AND task_id=? AND generation=1 AND operation='install'", task.OriginalID, task.ID).Scan(&requestData, &resultData); err != nil {
		return task, err
	}
	original, err := release.DecodeAgentUpdateRequest(requestData)
	if err != nil || original.ID != task.OriginalID || original.NodeID != task.NodeID || original.Revision != task.Revision {
		return task, release.ErrUpdate
	}
	if len(resultData) > 0 {
		result, err := release.DecodeUpdateResult(resultData)
		if err != nil || release.ValidateAgentResult(original, result) != nil {
			return task, release.ErrUpdate
		}
	}
	if len(resultData) == 0 && task.Generation == 1 && !confirm.Unconfirmed {
		return task, release.ErrUpdate
	}
	entry, err := catalogue.ReleaseTx(ctx, tx, task.ReleaseID)
	if err != nil || entry.Signed.Manifest != original.Release.Manifest {
		return task, release.ErrUpdate
	}
	commandID, err := id.New()
	if err != nil {
		return task, err
	}
	commandID = "arp_" + commandID
	request := release.AgentUpdateRequest{SchemaVersion: 1, ID: commandID, NodeID: task.NodeID, Revision: task.Revision, Operation: "recover", OriginalID: task.OriginalID, Release: original.Release}
	data, err := json.Marshal(request)
	if err != nil {
		return task, err
	}
	task.Generation++
	task.CommandID = commandID
	task.Status = "queued"
	task.Result = nil
	task.UpdatedAt = now()
	if _, err := tx.ExecContext(ctx, "INSERT INTO agent_update_attempts(command_id,task_id,generation,operation,request_json,created_at) VALUES(?,?,?,'recover',?,?)", commandID, task.ID, task.Generation, data, task.UpdatedAt); err != nil {
		return task, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE agent_update_tasks SET generation=?,command_id=?,status='queued',result_json=NULL,updated_at=? WHERE id=?", task.Generation, task.CommandID, task.UpdatedAt, task.ID); err != nil {
		return task, err
	}
	action := "retry_agent_recovery"
	if confirm.Unconfirmed {
		action = "recover_unconfirmed_agent"
	}
	if err := catalogue.RecordTx(ctx, tx, actor, action, task.ID, "成功", ip); err != nil {
		return task, err
	}
	return task, tx.Commit()
}

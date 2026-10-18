package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"proxycontrol/internal/platform/id"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

type AgentUpdateTask struct {
	ID         string                `json:"id"`
	NodeID     string                `json:"nodeId"`
	ReleaseID  string                `json:"releaseId"`
	Revision   int64                 `json:"revision"`
	Status     string                `json:"status"`
	Generation int64                 `json:"generation"`
	CommandID  string                `json:"commandId"`
	OriginalID string                `json:"originalId"`
	CreatedBy  string                `json:"createdBy"`
	CreatedAt  string                `json:"createdAt"`
	UpdatedAt  string                `json:"updatedAt"`
	Result     *release.UpdateResult `json:"result,omitempty"`
}

const agentTaskColumns = "id,node_id,release_id,baseline_revision,status,generation,command_id,original_command_id,created_by,created_at,updated_at,result_json"

func (r *Repository) AgentUpdateReserved(ctx context.Context, nodeID string) (bool, error) {
	var reserved bool
	err := r.Database.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM agent_update_tasks WHERE node_id=? AND status IN ('queued','running','failed_closed'))", nodeID).Scan(&reserved)
	return reserved, err
}

func scanAgentTask(row interface{ Scan(...any) error }) (AgentUpdateTask, error) {
	var t AgentUpdateTask
	var data []byte
	err := row.Scan(&t.ID, &t.NodeID, &t.ReleaseID, &t.Revision, &t.Status, &t.Generation, &t.CommandID, &t.OriginalID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &data)
	if err != nil {
		return t, err
	}
	if len(data) > 0 {
		result, err := release.DecodeUpdateResult(data)
		if err != nil {
			return t, err
		}
		t.Result = &result
	}
	return t, nil
}
func agentUpdateAvailable(ctx context.Context, tx *sql.Tx, nodeID string) error {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM agent_update_tasks WHERE node_id=? AND status IN ('queued','running','failed_closed')", nodeID).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return ErrNodeBusy
	}
	return nil
}
func (r *Repository) QueueAgentUpdate(ctx context.Context, catalogue ReleaseAccess, keys map[string]release.TrustedKey, nodeID, entryID, actor, ip string, expected int64) (AgentUpdateTask, error) {
	if expected < 1 || actor == "" || len(actor) > 128 {
		return AgentUpdateTask{}, release.ErrUpdate
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return AgentUpdateTask{}, err
	}
	defer tx.Rollback()
	if err := nodeAvailable(ctx, tx, nodeID, ""); err != nil {
		return AgentUpdateTask{}, err
	}
	var enabled, supported bool
	var platform string

	var revision int64
	err = tx.QueryRowContext(ctx, "SELECT n.enabled,n.operating_system,coalesce(c.agent_supported,0),s.revision FROM nodes n LEFT JOIN node_update_capabilities c ON c.node_id=n.id JOIN node_runtime_state s ON s.node_id=n.id WHERE n.id=?", nodeID).Scan(&enabled, &platform, &supported, &revision)
	if err != nil {
		return AgentUpdateTask{}, err
	}
	if !enabled || !supported || revision != expected {
		return AgentUpdateTask{}, ErrNodeBusy
	}
	entry, err := catalogue.ReleaseTx(ctx, tx, entryID)
	if err != nil {
		return AgentUpdateTask{}, err
	}
	m := entry.Signed.Manifest
	if m.Component != "proxy-agent" || platform != m.OS+"/"+m.Architecture {
		return AgentUpdateTask{}, release.ErrPolicy
	}
	if err := entry.Signed.Verify(release.Policy{Component: m.Component, OS: m.OS, Architecture: m.Architecture, Now: time.Now().UTC(), Keys: keys}); err != nil {
		return AgentUpdateTask{}, err
	}
	taskID, err := id.New()
	if err != nil {
		return AgentUpdateTask{}, err
	}
	commandID, err := id.New()
	if err != nil {
		return AgentUpdateTask{}, err
	}
	commandID = "aup_" + commandID
	t := AgentUpdateTask{ID: taskID, NodeID: nodeID, ReleaseID: entryID, Revision: expected, Status: "queued", Generation: 1, CommandID: commandID, OriginalID: commandID, CreatedBy: actor, CreatedAt: now(), UpdatedAt: now()}
	request := release.AgentUpdateRequest{SchemaVersion: 1, ID: commandID, NodeID: nodeID, Revision: expected, Operation: "install", Release: entry.Signed}
	data, _ := json.Marshal(request)
	if _, err := tx.ExecContext(ctx, "INSERT INTO agent_update_tasks("+agentTaskColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,NULL)", t.ID, t.NodeID, t.ReleaseID, t.Revision, t.Status, t.Generation, t.CommandID, t.OriginalID, t.CreatedBy, t.CreatedAt, t.UpdatedAt); err != nil {
		return t, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO agent_update_attempts(command_id,task_id,generation,operation,request_json,created_at) VALUES(?,?,1,'install',?,?)", commandID, taskID, data, t.CreatedAt); err != nil {
		return t, err
	}
	if err := catalogue.RecordTx(ctx, tx, actor, "queue_agent_update", taskID, "成功", ip); err != nil {
		return t, err
	}
	return t, tx.Commit()
}
func (r *Repository) ListAgentUpdates(ctx context.Context, page, size int) ([]AgentUpdateTask, int, error) {
	var total int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM agent_update_tasks").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT "+agentTaskColumns+" FROM agent_update_tasks ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []AgentUpdateTask{}
	for rows.Next() {
		t, err := scanAgentTask(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, t)
	}
	return items, total, rows.Err()
}
func (r *Repository) PendingAgentUpdate(ctx context.Context, nodeID string) (*pb.AgentUpdateCommand, error) {
	var data []byte
	err := r.Database.QueryRowContext(ctx, "SELECT a.request_json FROM agent_update_tasks t JOIN agent_update_attempts a ON a.command_id=t.command_id WHERE t.node_id=? AND t.status IN ('queued','running')", nodeID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	request, err := release.DecodeAgentUpdateRequest(data)
	if err != nil || request.NodeID != nodeID {
		return nil, release.ErrUpdate
	}
	if request.Operation == "recover" {
		var supported bool
		if err := r.Database.QueryRowContext(ctx, "SELECT agent_supported AND agent_recovery_supported FROM node_update_capabilities WHERE node_id=?", nodeID).Scan(&supported); err != nil {
			return nil, err
		}
		if !supported {
			return nil, nil
		}
	}
	if _, err := r.Database.ExecContext(ctx, "UPDATE agent_update_tasks SET status='running',updated_at=? WHERE command_id=? AND status='queued'", now(), request.ID); err != nil {
		return nil, err
	}
	return &pb.AgentUpdateCommand{RequestJson: data}, nil
}
func (r *Repository) AgentUpdateResult(ctx context.Context, nodeID string, payload *pb.AgentUpdateResult) error {
	if payload == nil {
		return release.ErrUpdate
	}
	result, err := release.DecodeUpdateResult(payload.ResultJson)
	if err != nil || result.NodeID != nodeID {
		return release.ErrUpdate
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var requestData, old []byte
	var taskID string
	if err := tx.QueryRowContext(ctx, "SELECT a.task_id,a.request_json,a.result_json FROM agent_update_attempts a JOIN agent_update_tasks t ON t.id=a.task_id WHERE a.command_id=? AND t.node_id=?", result.ID, nodeID).Scan(&taskID, &requestData, &old); err != nil {
		return err
	}
	request, err := release.DecodeAgentUpdateRequest(requestData)
	if err != nil || release.ValidateAgentResult(request, result) != nil {
		return release.ErrUpdate
	}
	if len(old) > 0 {
		previous, err := release.DecodeUpdateResult(old)
		if err != nil || previous != result {
			return release.ErrUpdate
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE agent_update_attempts SET result_json=? WHERE command_id=? AND result_json IS NULL", payload.ResultJson, result.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE agent_update_tasks SET status=?,result_json=?,updated_at=? WHERE id=? AND command_id=? AND status IN ('queued','running')", result.Status, payload.ResultJson, now(), taskID, result.ID); err != nil {
		return err
	}
	return tx.Commit()
}

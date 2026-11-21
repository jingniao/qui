package deployment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/id"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

type AgentSystemUpdateTask struct {
	ID            string                           `json:"id"`
	NodeID        string                           `json:"nodeId"`
	Revision      int64                            `json:"revision"`
	TargetVersion string                           `json:"targetVersion"`
	Status        string                           `json:"status"`
	Generation    int64                            `json:"generation"`
	CommandID     string                           `json:"commandId"`
	CreatedBy     string                           `json:"createdBy"`
	CreatedAt     string                           `json:"createdAt"`
	UpdatedAt     string                           `json:"updatedAt"`
	Phase         string                           `json:"phase"`
	SystemResult  *release.AgentSystemUpdateResult `json:"systemResult,omitempty"`
	DispatchError string                           `json:"dispatchError"`
	Mode          string                           `json:"mode"`
}

const systemTaskColumns = "id,node_id,baseline_revision,target_version,status,generation,attempt_id,created_by,created_at,updated_at,phase,result_json,dispatch_error"

func scanSystemTask(row interface{ Scan(...any) error }) (AgentSystemUpdateTask, error) {
	t := AgentSystemUpdateTask{Mode: "system"}
	var data []byte
	err := row.Scan(&t.ID, &t.NodeID, &t.Revision, &t.TargetVersion, &t.Status, &t.Generation, &t.CommandID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.Phase, &data, &t.DispatchError)
	if err != nil {
		return t, err
	}
	if len(data) > 0 {
		r, err := release.DecodeAgentSystemUpdateResult(data)
		if err != nil {
			return t, err
		}
		t.SystemResult = &r
	}
	return t, nil
}

// ListAllAgentUpdates pages both formats together while preserving the original
// worker-only result interpretation for published historical tasks.
func (r *Repository) ListAllAgentUpdates(ctx context.Context, page, size int) ([]any, int, error) {
	var total int
	if err := r.Database.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM agent_update_tasks)+(SELECT count(*) FROM agent_system_update_tasks)").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,mode FROM (SELECT id,'worker' mode,created_at FROM agent_update_tasks UNION ALL SELECT id,'system',created_at FROM agent_system_update_tasks) ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	type ref struct{ id, mode string }
	refs := []ref{}
	for rows.Next() {
		var v ref
		if err = rows.Scan(&v.id, &v.mode); err != nil {
			rows.Close()
			return nil, 0, err
		}
		refs = append(refs, v)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return nil, 0, err
	}
	items := []any{}
	for _, v := range refs {
		if v.mode == "system" {
			t, err := scanSystemTask(r.Database.QueryRowContext(ctx, "SELECT "+systemTaskColumns+" FROM agent_system_update_tasks WHERE id=?", v.id))
			if err != nil {
				return nil, 0, err
			}
			items = append(items, t)
		} else {
			t, err := scanAgentTask(r.Database.QueryRowContext(ctx, "SELECT "+agentTaskColumns+" FROM agent_update_tasks WHERE id=?", v.id))
			if err != nil {
				return nil, 0, err
			}
			items = append(items, struct {
				AgentUpdateTask
				Mode string `json:"mode"`
			}{t, "worker"})
		}
	}
	return items, total, nil
}
func (r *Repository) QueueAgentSystemUpdate(ctx context.Context, audit ReleaseAccess, nodeID, target, actor, ip string, revision int64) (AgentSystemUpdateTask, error) {
	taskID, err := id.New()
	if err != nil {
		return AgentSystemUpdateTask{}, err
	}
	attemptID, err := id.New()
	if err != nil {
		return AgentSystemUpdateTask{}, err
	}
	q := release.AgentSystemUpdateRequest{SchemaVersion: 1, Operation: "upgrade", TaskID: taskID, AttemptID: attemptID, NodeID: nodeID, Revision: revision, TargetVersion: target}
	if err = q.Validate(); err != nil {
		return AgentSystemUpdateTask{}, err
	}
	if actor == "" || len(actor) > 128 {
		return AgentSystemUpdateTask{}, fmt.Errorf("missing update author")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return AgentSystemUpdateTask{}, err
	}
	defer tx.Rollback()
	if err := requireAgentVersion(ctx, tx, nodeID, target); err != nil {
		return AgentSystemUpdateTask{}, err
	}
	if err = nodeAvailable(ctx, tx, nodeID, ""); err != nil {
		return AgentSystemUpdateTask{}, fmt.Errorf("node has a conflicting configuration or update operation: %w", err)
	}
	if err = systemNodeReady(ctx, tx, nodeID, revision); err != nil {
		return AgentSystemUpdateTask{}, err
	}
	t := AgentSystemUpdateTask{ID: taskID, NodeID: nodeID, Revision: revision, TargetVersion: target, Status: "queued", Generation: 1, CommandID: attemptID, CreatedBy: actor, CreatedAt: now(), UpdatedAt: now(), Mode: "system"}
	if err = insertSystemAttempt(ctx, tx, t, q); err != nil {
		return t, err
	}
	if err = audit.RecordTx(ctx, tx, actor, "queue_agent_system_update", taskID, "成功", ip); err != nil {
		return t, err
	}
	return t, tx.Commit()
}
func systemNodeReady(ctx context.Context, tx *sql.Tx, nodeID string, revision int64) error {
	var enabled, supported, connected bool
	var actual int64
	var reason string
	if err := tx.QueryRowContext(ctx, `SELECT n.enabled,coalesce(c.agent_system_supported,0),coalesce(c.agent_system_error,''),s.revision,n.status IN ('online','degraded') AND n.last_heartbeat>=? FROM nodes n JOIN node_runtime_state s ON s.node_id=n.id LEFT JOIN node_update_capabilities c ON c.node_id=n.id WHERE n.id=? AND coalesce(n.deleted_at,'')=''`, time.Now().UTC().Add(-45*time.Second).Format("2006-01-02T15:04:05.000000000Z"), nodeID).Scan(&enabled, &supported, &reason, &actual, &connected); err != nil {
		return err
	}
	if !enabled || !connected {
		return fmt.Errorf("node is disabled or has no control heartbeat within the last 45 seconds")
	}
	if actual != revision {
		return fmt.Errorf("node business revision changed: expected %d, actual %d", revision, actual)
	}
	if !supported {
		return fmt.Errorf("complete system update is unavailable: %s; run qui-update --component agent on this node", reason)
	}
	return nil
}
func insertSystemAttempt(ctx context.Context, tx *sql.Tx, t AgentSystemUpdateTask, q release.AgentSystemUpdateRequest) error {
	data, err := json.Marshal(q)
	if err != nil {
		return err
	}
	if t.Generation == 1 {
		_, err = tx.ExecContext(ctx, "INSERT INTO agent_system_update_tasks("+systemTaskColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,NULL,?)", t.ID, t.NodeID, t.Revision, t.TargetVersion, t.Status, t.Generation, t.CommandID, t.CreatedBy, t.CreatedAt, t.UpdatedAt, t.Phase, t.DispatchError)
		if err != nil {
			return err
		}
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE agent_system_update_tasks SET status='queued',generation=?,attempt_id=?,updated_at=?,phase='',result_json=NULL,dispatch_error='' WHERE id=?", t.Generation, t.CommandID, t.UpdatedAt, t.ID)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO agent_system_update_attempts(attempt_id,task_id,generation,request_json,created_at) VALUES(?,?,?,?,?)", t.CommandID, t.ID, t.Generation, data, t.UpdatedAt)
	return err
}
func (r *Repository) RetryAgentSystemUpdate(ctx context.Context, audit ReleaseAccess, taskID, actor, ip string, c AgentRecoveryConfirmation) (AgentSystemUpdateTask, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return AgentSystemUpdateTask{}, err
	}
	defer tx.Rollback()
	t, err := scanSystemTask(tx.QueryRowContext(ctx, "SELECT "+systemTaskColumns+" FROM agent_system_update_tasks WHERE id=?", taskID))
	if err != nil {
		return t, err
	}
	if t.Status != "failed" || t.SystemResult == nil || t.NodeID != c.NodeID || t.Revision != c.Revision || t.CommandID != c.CommandID || t.Generation != c.Generation {
		return t, fmt.Errorf("retry requires the current confirmed failed attempt; reconcile unknown execution first")
	}
	if t.SystemResult.Recovery.Attempted && (t.SystemResult.Recovery.Success == nil || !*t.SystemResult.Recovery.Success) {
		return t, fmt.Errorf("original updater recovery is not confirmed successful: %s", t.SystemResult.Recovery.Error)
	}
	if err := requireAgentVersion(ctx, tx, t.NodeID, t.TargetVersion); err != nil {
		return t, err
	}
	if err = nodeAvailable(ctx, tx, t.NodeID, ""); err != nil {
		return t, err
	}
	if err = systemNodeReady(ctx, tx, t.NodeID, t.Revision); err != nil {
		return t, err
	}
	attempt, err := id.New()
	if err != nil {
		return t, err
	}
	t.CommandID = attempt
	t.Generation++
	t.Status = "queued"
	t.SystemResult = nil
	t.DispatchError = ""
	t.Phase = ""
	t.UpdatedAt = now()
	q := release.AgentSystemUpdateRequest{SchemaVersion: 1, Operation: "upgrade", TaskID: t.ID, AttemptID: t.CommandID, NodeID: t.NodeID, Revision: t.Revision, TargetVersion: t.TargetVersion}
	if err = insertSystemAttempt(ctx, tx, t, q); err != nil {
		return t, err
	}
	if err = audit.RecordTx(ctx, tx, actor, "retry_agent_system_update", t.ID, "成功", ip); err != nil {
		return t, err
	}
	return t, tx.Commit()
}
func (r *Repository) PendingAgentSystemUpdate(ctx context.Context, nodeID string) (*pb.AgentSystemUpdateCommand, error) {
	var data []byte
	err := r.Database.QueryRowContext(ctx, "SELECT a.request_json FROM agent_system_update_tasks t JOIN agent_system_update_attempts a ON a.attempt_id=t.attempt_id WHERE t.node_id=? AND t.status IN ('queued','running','unconfirmed')", nodeID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	q, err := release.DecodeAgentSystemUpdateRequest(data)
	if err != nil {
		return nil, err
	}
	if q.NodeID != nodeID {
		return nil, fmt.Errorf("pending system update node mismatch")
	}
	if err := requireAgentVersion(ctx, r.Database, nodeID, q.TargetVersion); err != nil {
		return nil, err
	}
	_, err = r.Database.ExecContext(ctx, "UPDATE agent_system_update_tasks SET status='running',updated_at=? WHERE attempt_id=? AND status='queued'", now(), q.AttemptID)
	return &pb.AgentSystemUpdateCommand{RequestJson: data}, err
}
func (r *Repository) AgentSystemUpdateEvent(ctx context.Context, nodeID string, e *pb.AgentSystemUpdateEvent) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("missing system update event")
	}
	count := 0
	for _, data := range [][]byte{e.ProgressJson, e.ResultJson, e.LogChunkJson} {
		if len(data) > 0 {
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("system update event must contain one observation")
	}
	var attempt string
	var p release.AgentSystemUpdateProgress
	var result release.AgentSystemUpdateResult
	var chunk release.AgentSystemUpdateLogChunk
	var err error
	if len(e.ProgressJson) > 0 {
		p, err = release.DecodeAgentSystemUpdateProgress(e.ProgressJson)
		attempt = p.AttemptID
		if p.Phase == "" {
			return nil, fmt.Errorf("invalid system update progress")
		}
		if _, err := time.Parse(time.RFC3339Nano, p.ObservedAt); err != nil {
			return nil, err
		}
	}
	if len(e.ResultJson) > 0 {
		result, err = release.DecodeAgentSystemUpdateResult(e.ResultJson)
		attempt = result.AttemptID
	}
	if len(e.LogChunkJson) > 0 {
		chunk, err = release.DecodeAgentSystemUpdateLogChunk(e.LogChunkJson)
		attempt = chunk.AttemptID
		if err == nil {
			err = chunk.Validate()
		}
	}
	if err != nil {
		return nil, err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var qData, oldResult, oldProgress, oldDispatchError []byte
	var ack release.AgentSystemUpdateAck
	ack.AttemptID = attempt
	if err = tx.QueryRowContext(ctx, "SELECT a.request_json,a.result_json,a.progress_json,a.log_sequence,a.log_offset,a.dispatch_error_json FROM agent_system_update_attempts a JOIN agent_system_update_tasks t ON t.id=a.task_id WHERE a.attempt_id=? AND t.node_id=?", attempt, nodeID).Scan(&qData, &oldResult, &oldProgress, &ack.LogSequence, &ack.LogOffset, &oldDispatchError); err != nil {
		return nil, err
	}
	q, err := release.DecodeAgentSystemUpdateRequest(qData)
	if err != nil {
		return nil, err
	}
	if len(oldProgress) > 0 {
		var old release.AgentSystemUpdateProgress
		if err = json.Unmarshal(oldProgress, &old); err != nil {
			return nil, err
		}
		ack.ProgressSequence = old.Sequence
	}
	if len(oldResult) > 0 {
		d := sha256.Sum256(oldResult)
		ack.ResultSHA256 = hex.EncodeToString(d[:])
	}
	if len(oldDispatchError) > 0 {
		d := sha256.Sum256(oldDispatchError)
		ack.DispatchErrorSHA256 = hex.EncodeToString(d[:])
	}
	if len(e.ProgressJson) > 0 && p.Sequence == 0 {
		if len(oldDispatchError) > 0 && !bytes.Equal(oldDispatchError, e.ProgressJson) {
			return nil, fmt.Errorf("system update dispatch error was replaced")
		}
		if len(oldDispatchError) == 0 {
			if _, err = tx.ExecContext(ctx, "UPDATE agent_system_update_attempts SET dispatch_error_json=? WHERE attempt_id=?", e.ProgressJson, attempt); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE agent_system_update_tasks SET dispatch_error=?,phase=CASE WHEN phase='' THEN 'request_submit' ELSE phase END,status='unconfirmed',updated_at=? WHERE attempt_id=? AND status IN ('queued','running','unconfirmed')", p.Error, now(), attempt); err != nil {
				return nil, err
			}
		}
		d := sha256.Sum256(e.ProgressJson)
		ack.DispatchErrorSHA256 = hex.EncodeToString(d[:])
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return json.Marshal(ack)
	}
	if len(e.ProgressJson) > 0 {
		if p.Sequence <= ack.ProgressSequence {
			if !bytes.Equal(oldProgress, e.ProgressJson) {
				return nil, fmt.Errorf("system update progress sequence reused")
			}
		} else {
			if _, err = tx.ExecContext(ctx, "UPDATE agent_system_update_attempts SET progress_json=? WHERE attempt_id=?", e.ProgressJson, attempt); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE agent_system_update_tasks SET phase=?,updated_at=?,status=CASE WHEN ?='interrupted' THEN 'unconfirmed' ELSE status END WHERE attempt_id=? AND status IN ('queued','running','unconfirmed')", p.Phase, now(), p.Phase, attempt); err != nil {
				return nil, err
			}
			ack.ProgressSequence = p.Sequence
		}
	}
	if len(e.LogChunkJson) > 0 {
		if chunk.Sequence <= ack.LogSequence {
			var old []byte
			var offset int64
			var digest string
			if err = tx.QueryRowContext(ctx, "SELECT content,offset,sha256 FROM agent_system_update_logs WHERE attempt_id=? AND sequence=?", attempt, chunk.Sequence).Scan(&old, &offset, &digest); err != nil {
				return nil, err
			}
			if !bytes.Equal(old, chunk.Content) || offset != chunk.Offset || digest != chunk.SHA256 {
				return nil, fmt.Errorf("system update log sequence reused with different content")
			}
		} else {
			if chunk.Sequence != ack.LogSequence+1 || chunk.Offset != ack.LogOffset {
				return nil, fmt.Errorf("system update log gap at byte %d", ack.LogOffset)
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO agent_system_update_logs(attempt_id,sequence,offset,sha256,content) VALUES(?,?,?,?,?)", attempt, chunk.Sequence, chunk.Offset, chunk.SHA256, chunk.Content); err != nil {
				return nil, err
			}
			ack.LogSequence = chunk.Sequence
			ack.LogOffset += int64(len(chunk.Content))
			if _, err = tx.ExecContext(ctx, "UPDATE agent_system_update_attempts SET log_sequence=?,log_offset=? WHERE attempt_id=?", ack.LogSequence, ack.LogOffset, attempt); err != nil {
				return nil, err
			}
		}
	}
	if len(e.ResultJson) > 0 {
		if result.NodeID != q.NodeID || result.TaskID != q.TaskID || result.Revision != q.Revision || result.TargetVersion != q.TargetVersion {
			return nil, fmt.Errorf("system update result does not match authorized request")
		}
		if len(oldResult) > 0 && !bytes.Equal(oldResult, e.ResultJson) {
			return nil, fmt.Errorf("system update attempt has a different persisted conclusion")
		}
		if len(oldResult) == 0 {
			if _, err = tx.ExecContext(ctx, "UPDATE agent_system_update_attempts SET result_json=? WHERE attempt_id=?", e.ResultJson, attempt); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE agent_system_update_tasks SET status=?,phase=?,result_json=?,updated_at=? WHERE attempt_id=?", result.Status, result.Phase, e.ResultJson, now(), attempt); err != nil {
				return nil, err
			}
		}
		d := sha256.Sum256(e.ResultJson)
		ack.ResultSHA256 = hex.EncodeToString(d[:])
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return json.Marshal(ack)
}

type AgentSystemUpdateAttempt struct {
	DispatchError string                           `json:"dispatchError"`
	Status        string                           `json:"status"`
	Current       bool                             `json:"current"`
	Operation     string                           `json:"operation"`
	CommandID     string                           `json:"commandId"`
	Generation    int64                            `json:"generation"`
	CreatedAt     string                           `json:"createdAt"`
	SystemResult  *release.AgentSystemUpdateResult `json:"systemResult,omitempty"`
	Phase         string                           `json:"phase"`
}

func (r *Repository) ListAgentSystemAttempts(ctx context.Context, taskID string, page, size int) ([]AgentSystemUpdateAttempt, int, error) {
	tx, err := db.BeginRead(ctx, r.Database)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Close()
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM agent_system_update_attempts WHERE task_id=?", taskID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT a.attempt_id,a.generation,a.created_at,a.result_json,a.progress_json,a.attempt_id=t.attempt_id,t.status,coalesce(json_extract(a.dispatch_error_json,'$.error'),'') FROM agent_system_update_attempts a JOIN agent_system_update_tasks t ON t.id=a.task_id WHERE a.task_id=? ORDER BY a.generation DESC LIMIT ? OFFSET ?", taskID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []AgentSystemUpdateAttempt{}
	for rows.Next() {
		var item AgentSystemUpdateAttempt
		var currentStatus string
		var result, progress []byte
		if err = rows.Scan(&item.CommandID, &item.Generation, &item.CreatedAt, &result, &progress, &item.Current, &currentStatus, &item.DispatchError); err != nil {
			return nil, 0, err
		}
		if len(result) > 0 {
			v, err := release.DecodeAgentSystemUpdateResult(result)
			if err != nil {
				return nil, 0, err
			}
			item.SystemResult = &v
			item.Status = v.Status
		}
		if item.Current {
			item.Status = currentStatus
		} else if item.SystemResult == nil {
			item.Status = "unknown"
		}
		item.Operation = "upgrade"
		if item.Generation > 1 {
			item.Operation = "retry"
		}
		if len(progress) > 0 {
			var p release.AgentSystemUpdateProgress
			if err = json.Unmarshal(progress, &p); err != nil {
				return nil, 0, err
			}
			item.Phase = p.Phase
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
func (r *Repository) WriteAgentSystemLogs(ctx context.Context, taskID, attempt string, offset int64, w io.Writer) (int64, bool, string, error) {
	tx, err := db.BeginRead(ctx, r.Database)
	if err != nil {
		return offset, false, "", err
	}
	defer tx.Close()
	var resultData []byte
	var end int64
	if attempt == "" {
		if err := tx.QueryRowContext(ctx, "SELECT attempt_id FROM agent_system_update_tasks WHERE id=?", taskID).Scan(&attempt); err != nil {
			return offset, false, "", err
		}
	}
	if err := tx.QueryRowContext(ctx, "SELECT a.log_offset,a.result_json FROM agent_system_update_attempts a WHERE a.task_id=? AND a.attempt_id=?", taskID, attempt).Scan(&end, &resultData); err != nil {
		return offset, false, "", err
	}
	if offset < 0 || offset > end {
		return offset, false, "", fmt.Errorf("log offset is outside persisted output")
	}
	rows, err := tx.QueryContext(ctx, "SELECT offset,content FROM agent_system_update_logs WHERE attempt_id=? AND offset+length(content)>? ORDER BY sequence", attempt, offset)
	if err != nil {
		return offset, false, "", err
	}
	defer rows.Close()
	position := offset
	for rows.Next() {
		var start int64
		var data []byte
		if err = rows.Scan(&start, &data); err != nil {
			return position, false, "", err
		}
		if start > position {
			return position, false, "log gap in persisted output", nil
		}
		skip := position - start
		if skip < 0 {
			skip = 0
		}
		n, err := w.Write(data[skip:])
		position += int64(n)
		if err != nil {
			return position, false, "", err
		}
	}
	if err = rows.Err(); err != nil {
		return position, false, "", err
	}
	complete := false
	logError := ""
	if len(resultData) > 0 {
		result, err := release.DecodeAgentSystemUpdateResult(resultData)
		if err != nil {
			return position, false, "", err
		}
		complete = position == result.LogBytes && end == result.LogBytes
		logError = result.LogError
		if end != result.LogBytes && logError == "" {
			logError = fmt.Sprintf("persisted updater output has %d bytes; root receipt declares %d bytes", end, result.LogBytes)
		}
	}
	return position, complete, logError, nil
}

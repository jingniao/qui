package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"google.golang.org/protobuf/proto"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/internal/platform/id"
	pb "proxycontrol/pkg/contracts/agentpb"
)

type CoreRemoveTask struct {
	ID        string               `json:"id"`
	NodeID    string               `json:"nodeId"`
	Component string               `json:"component"`
	Revision  int64                `json:"revision"`
	Status    string               `json:"status"`
	Code      string               `json:"code"`
	CreatedBy string               `json:"createdBy"`
	CreatedAt string               `json:"createdAt"`
	UpdatedAt string               `json:"updatedAt"`
	Result    *pb.RemoveCoreResult `json:"result,omitempty"`
}
type CoreRemovalConflict struct {
	Code       string   `json:"code"`
	InboundIDs []string `json:"inboundIds"`
}

func (e *CoreRemovalConflict) Error() string { return e.Code }

const removeColumns = "id,node_id,component,revision,status,code,created_by,created_at,updated_at,result_json"

func scanRemove(row interface{ Scan(...any) error }) (CoreRemoveTask, error) {
	var t CoreRemoveTask
	var raw []byte
	err := row.Scan(&t.ID, &t.NodeID, &t.Component, &t.Revision, &t.Status, &t.Code, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &raw)
	if err == nil && len(raw) > 0 {
		err = json.Unmarshal(raw, &t.Result)
	}
	return t, err
}
func (r *Repository) QueueRemoveCore(ctx context.Context, audit ReleaseAccess, nodeID, component, actor, ip string, revision int64) (CoreRemoveTask, error) {
	conflict := func(code string) (CoreRemoveTask, error) {
		return CoreRemoveTask{}, &CoreRemovalConflict{Code: code, InboundIDs: []string{}}
	}
	if !node.CoreNameValid(component) || revision < 1 {
		return conflict("invalid_request")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return CoreRemoveTask{}, err
	}
	defer tx.Rollback()
	var caps []byte
	var current int64
	var supported bool
	if err := tx.QueryRowContext(ctx, "SELECT n.capabilities_json,s.revision,n.multi_core_supported FROM nodes n JOIN node_runtime_state s ON s.node_id=n.id WHERE n.id=? AND n.enabled=1 AND n.deleted_at IS NULL", nodeID).Scan(&caps, &current, &supported); err != nil {
		return CoreRemoveTask{}, err
	}
	if current != revision {
		return conflict("revision_mismatch")
	}
	c := &pb.Capabilities{}
	if storedJSON.Unmarshal(caps, c) != nil || !supported || !c.MultiCoreSupported || node.SelectedCore(c, component) == nil {
		return conflict("unsupported")
	}
	if len(node.EnabledCores(c)) <= 1 {
		return conflict("last_core")
	}
	if err := nodeAvailable(ctx, tx, nodeID, ""); err != nil {
		return conflict("core_pending")
	}
	used := map[string]bool{}
	rows, err := tx.QueryContext(ctx, "SELECT id,core FROM inbounds WHERE node_id=?", nodeID)
	if err != nil {
		return CoreRemoveTask{}, err
	}
	for rows.Next() {
		var id, core string
		if err := rows.Scan(&id, &core); err != nil {
			rows.Close()
			return CoreRemoveTask{}, err
		}
		if core == component || core == "" && c.CoreName == component {
			used[id] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return CoreRemoveTask{}, err
	}
	state, err := r.stateInTransaction(ctx, tx, current)
	if err != nil {
		return CoreRemoveTask{}, err
	}
	for _, in := range state.Inbounds {
		if in.Core == component || in.Core == "" && c.CoreName == component {
			used[in.ID] = true
		}
	}
	if len(used) > 0 {
		ids := []string{}
		for id := range used {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return CoreRemoveTask{}, &CoreRemovalConflict{Code: "core_in_use", InboundIDs: ids}
	}
	taskID, err := id.New()
	if err != nil {
		return CoreRemoveTask{}, err
	}
	t := CoreRemoveTask{ID: taskID, NodeID: nodeID, Component: component, Revision: revision, Status: "queued", CreatedBy: actor, CreatedAt: now(), UpdatedAt: now()}
	if _, err := tx.ExecContext(ctx, "INSERT INTO core_remove_tasks("+removeColumns+") VALUES(?,?,?,?,'queued','',?,?,?,NULL)", t.ID, t.NodeID, t.Component, t.Revision, t.CreatedBy, t.CreatedAt, t.UpdatedAt); err != nil {
		return t, err
	}
	if err := audit.RecordTx(ctx, tx, actor, "remove_core", t.ID, "成功", ip); err != nil {
		return t, err
	}
	return t, tx.Commit()
}
func (r *Repository) PendingRemoveCore(ctx context.Context, nodeID string) (*pb.RemoveCoreCommand, error) {
	t, err := scanRemove(r.Database.QueryRowContext(ctx, "SELECT "+removeColumns+" FROM core_remove_tasks WHERE node_id=? AND status IN ('queued','running') ORDER BY updated_at,id LIMIT 1", nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := r.Database.ExecContext(ctx, "UPDATE core_remove_tasks SET status='running',updated_at=? WHERE id=?", now(), t.ID); err != nil {
		return nil, err
	}
	return &pb.RemoveCoreCommand{Id: t.ID, Component: t.Component, Revision: t.Revision}, nil
}
func (r *Repository) RemoveCoreResult(ctx context.Context, nodeID string, result *pb.RemoveCoreResult) error {
	if result == nil || len(result.Code) > 128 || result.Code == "" || result.Success != (result.Code == "success") {
		return errors.New("missing remove result")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := scanRemove(tx.QueryRowContext(ctx, "SELECT "+removeColumns+" FROM core_remove_tasks WHERE id=? AND node_id=?", result.Id, nodeID))
	if err != nil {
		return err
	}
	if result.Component != t.Component || result.Revision != t.Revision {
		return errors.New("remove identity mismatch")
	}
	if t.Result != nil {
		if !proto.Equal(t.Result, result) {
			return errors.New("remove result changed")
		}
		return nil
	}
	if t.Status != "queued" && t.Status != "running" && t.Status != "unconfirmed" {
		return errors.New("remove task closed")
	}
	status := "rejected"
	if result.Success {
		status = "success"
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE core_remove_tasks SET status=?,code=?,result_json=?,updated_at=? WHERE id=?", status, result.Code, raw, now(), t.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (h *CoreUpdateHandler) remove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConfirmNodeID string `json:"confirmNodeId"`
		Revision      int64  `json:"revision"`
	}
	if httpapi.Decode(w, r, &in) != nil || in.ConfirmNodeID != r.PathValue("id") {
		httpapi.Error(w, 400, "invalid_request", "Confirm the target node and revision")
		return
	}
	t, err := h.Repository.QueueRemoveCore(r.Context(), h.Audit, r.PathValue("id"), r.PathValue("component"), auth.Actor(r.Context()).Username, r.RemoteAddr, in.Revision)
	if err != nil {
		var conflict *CoreRemovalConflict
		if errors.As(err, &conflict) {
			httpapi.ErrorDetails(w, 409, conflict.Code, "Core removal refused", map[string]any{"inboundIds": conflict.InboundIDs})
			return
		}
		httpapi.Error(w, 409, "core_pending", "Core operation unavailable")
		return
	}
	httpapi.JSON(w, 202, t)
}
func (h *CoreUpdateHandler) removals(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Repository.Database.QueryContext(r.Context(), "SELECT "+removeColumns+" FROM core_remove_tasks WHERE node_id=? AND component=? ORDER BY created_at DESC LIMIT 100", r.PathValue("id"), r.PathValue("component"))
	if err != nil {
		httpapi.Error(w, 500, "internal_error", "Core removal tasks unavailable")
		return
	}
	defer rows.Close()
	items := []CoreRemoveTask{}
	for rows.Next() {
		t, err := scanRemove(rows)
		if err != nil {
			httpapi.Error(w, 500, "internal_error", "Core removal tasks unavailable")
			return
		}
		items = append(items, t)
	}
	if rows.Err() != nil {
		httpapi.Error(w, 500, "internal_error", "Core removal tasks unavailable")
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}

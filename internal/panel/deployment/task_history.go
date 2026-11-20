package deployment

import (
	"context"
	"proxycontrol/internal/platform/db"

	"errors"
	"net/http"
	"strings"

	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/release"
)

var errTaskHistoryFilter = errors.New("invalid task type or status")

type TaskRecord struct {
	DispatchError string                           `json:"dispatchError"`
	Mode          string                           `json:"mode"`
	Phase         string                           `json:"phase"`
	SystemResult  *release.AgentSystemUpdateResult `json:"systemResult,omitempty"`
	SystemUpdate  *AgentSystemUpdateTask           `json:"systemUpdate,omitempty"`
	ID            string                           `json:"id"`
	Source        string                           `json:"source"`
	Component     string                           `json:"component"`
	NodeID        string                           `json:"nodeId"`
	NodeName      string                           `json:"nodeName"`
	Revision      int64                            `json:"revision"`
	TargetVersion string                           `json:"targetVersion"`
	Status        string                           `json:"status"`
	UpdatedAt     string                           `json:"updatedAt"`
	Result        *release.UpdateResult            `json:"result,omitempty"`
	Recovery      *AgentUpdateTask                 `json:"recovery,omitempty"`
}

// Both task sources are filtered and paginated in one read snapshot. Only the
// displayed page carries recovery tokens; no full node or release catalogue is read.
const historyCTE = `WITH tasks AS (
 SELECT 'agent' source,t.id,t.node_id,'agent' component,t.baseline_revision revision,
 coalesce(json_extract(c.signed_json,'$.manifest.version'),json_extract(t.result_json,'$.installed_version'),'—') version,
 t.status,t.updated_at,t.result_json,t.release_id,t.generation,t.command_id,t.original_command_id,t.created_by,t.created_at,'worker' mode,'' phase,'' dispatch_error
 FROM agent_update_tasks t LEFT JOIN release_catalogue c ON c.id=t.release_id
 UNION ALL
 SELECT 'core',id,node_id,component,baseline_revision,version,status,updated_at,result_json,'',0,command_id,'',created_by,created_at,'worker','','' FROM core_update_tasks
 UNION ALL
 SELECT 'agent',id,node_id,'agent',baseline_revision,target_version,status,updated_at,result_json,'',generation,attempt_id,'',created_by,created_at,'system',phase,dispatch_error FROM agent_system_update_tasks
), records AS (
 SELECT t.*,coalesce(n.name,t.node_id) node_name,
 CASE WHEN source='core' THEN component||' '||version WHEN version='—' OR substr(version,1,1)='v' THEN version ELSE 'v'||version END target_version,
 CASE t.status WHEN 'success' THEN '升级成功' WHEN 'running' THEN '下发中，待确认' WHEN 'queued' THEN '等待节点' WHEN 'rolled_back' THEN CASE source WHEN 'agent' THEN '已恢复原 Agent' ELSE '已回退上一核心' END WHEN 'rejected' THEN '节点拒绝' WHEN 'failed_closed' THEN '未确认已关闭' WHEN 'cleared' THEN '已清除' ELSE t.status END status_label
 FROM tasks t LEFT JOIN nodes n ON n.id=t.node_id
) `

func (r *Repository) TaskHistory(ctx context.Context, kind, status, keyword string, page, size int) ([]TaskRecord, int, error) {
	where := " WHERE 1=1"
	args := []any{}
	if kind != "all" && kind != "" {
		switch kind {
		case "agent", "xray", "sing-box", "gost":
		default:
			return nil, 0, errTaskHistoryFilter
		}
		where += " AND component=?"
		args = append(args, kind)
	}
	if status != "all" && status != "" {
		switch status {
		case "failed":
			where += " AND status IN ('rejected','failed_closed','failed')"
		case "running", "queued", "success", "rolled_back", "cleared", "unconfirmed":
			where += " AND status=?"
			args = append(args, status)
		default:
			return nil, 0, errTaskHistoryFilter
		}
	}
	if keyword != "" {
		where += " AND (instr(lower(node_name),?)>0 OR instr(lower(target_version),?)>0 OR instr(lower(status_label),?)>0 OR instr(lower(coalesce(json_extract(result_json,'$.installed_version'),'')),?)>0)"
		for i := 0; i < 4; i++ {
			args = append(args, strings.ToLower(keyword))
		}
	}
	tx, err := db.BeginRead(ctx, r.Database)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Close()
	var total int
	if err = tx.QueryRowContext(ctx, historyCTE+"SELECT count(*) FROM records"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), size, (page-1)*size)
	rows, err := tx.QueryContext(ctx, historyCTE+`SELECT id,source,component,node_id,node_name,revision,target_version,status,updated_at,result_json,release_id,generation,command_id,original_command_id,created_by,created_at,mode,phase,dispatch_error FROM records`+where+" ORDER BY updated_at DESC,source,id DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []TaskRecord{}
	for rows.Next() {
		var item TaskRecord
		var recovery AgentUpdateTask
		var data []byte
		if err = rows.Scan(&item.ID, &item.Source, &item.Component, &item.NodeID, &item.NodeName, &item.Revision, &item.TargetVersion, &item.Status, &item.UpdatedAt, &data, &recovery.ReleaseID, &recovery.Generation, &recovery.CommandID, &recovery.OriginalID, &recovery.CreatedBy, &recovery.CreatedAt, &item.Mode, &item.Phase, &item.DispatchError); err != nil {
			return nil, 0, err
		}
		if item.Mode == "system" {
			system := AgentSystemUpdateTask{ID: item.ID, NodeID: item.NodeID, Revision: item.Revision, TargetVersion: strings.TrimPrefix(item.TargetVersion, "v"), Status: item.Status, Generation: recovery.Generation, CommandID: recovery.CommandID, CreatedBy: recovery.CreatedBy, CreatedAt: recovery.CreatedAt, UpdatedAt: item.UpdatedAt, Mode: "system", Phase: item.Phase, DispatchError: item.DispatchError}
			if len(data) > 0 {
				result, err := release.DecodeAgentSystemUpdateResult(data)
				if err != nil {
					return nil, 0, err
				}
				item.SystemResult = &result
				system.SystemResult = &result
			}
			item.SystemUpdate = &system
			items = append(items, item)
			continue
		}
		if len(data) > 0 {
			result, err := release.DecodeUpdateResult(data)
			if err != nil {
				return nil, 0, err
			}
			item.Result = &result
		}
		if item.Source == "agent" {
			recovery.ID = item.ID
			recovery.NodeID = item.NodeID
			recovery.Revision = item.Revision
			recovery.Status = item.Status
			recovery.UpdatedAt = item.UpdatedAt
			recovery.Result = item.Result
			item.Recovery = &recovery
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (h *Handler) taskHistory(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	q := r.URL.Query()
	items, total, err := h.Repository.TaskHistory(r.Context(), q.Get("type"), q.Get("status"), q.Get("keyword"), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

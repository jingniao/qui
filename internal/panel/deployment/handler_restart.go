package deployment

import (
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) restartNode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		ConfirmNodeID    string `json:"confirmNodeId"`
		Core             string `json:"core"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil || input.ExpectedRevision < 1 || input.ConfirmNodeID != r.PathValue("id") {
		httpapi.Error(w, 400, "invalid_request", "Current revision and matching node confirmation required")
		return
	}
	command, err := h.Repository.RestartCore(r.Context(), input.ConfirmNodeID, input.ExpectedRevision, input.Core)
	if err != nil {
		httpapi.Error(w, 409, "restart_conflict", err.Error())
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "申请重启当前核心", input.ConfirmNodeID, "已受理", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"commandId": command.CommandId, "revision": command.Revision, "status": "pending", "core": command.Core})
}
func (h *Handler) restartTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Repository.Database.QueryContext(r.Context(), `SELECT d.id,d.revision,d.status,d.result_code,d.created_at,t.core FROM node_restart_requests t JOIN deployment_commands d ON d.id=t.command_id WHERE t.node_id=? ORDER BY d.created_at DESC LIMIT 10`, r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, status, code, created, core string
		var revision int64
		if err := rows.Scan(&id, &revision, &status, &code, &created, &core); err != nil {
			h.fail(w, err)
			return
		}
		items = append(items, map[string]any{"commandId": id, "revision": revision, "status": status, "code": code, "createdAt": created, "core": core})
	}
	if err := rows.Err(); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}

package deployment

import (
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

type AgentUpdateHandler struct {
	Repository *Repository
	Catalogue  ReleaseAccess
	Auth       *auth.Handler
}

func (h *AgentUpdateHandler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/agent-updates", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("POST /api/v1/agent-updates", h.Auth.Require(http.HandlerFunc(h.systemQueue), true))
	mux.Handle("POST /api/v1/agent-updates/{id}/retry", h.Auth.Require(http.HandlerFunc(h.systemRetry), true))
	mux.Handle("GET /api/v1/agent-updates/{id}/logs", h.Auth.Require(http.HandlerFunc(h.systemLogs), true))
	mux.Handle("POST /api/v1/agent-updates/{id}/recover", h.Auth.Require(http.HandlerFunc(h.recover), true))
	mux.Handle("GET /api/v1/agent-updates/{id}/attempts", h.Auth.Require(http.HandlerFunc(h.attempts), true))
}

func (h *AgentUpdateHandler) attempts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "分页参数无效")
		return
	}
	var system bool
	if err := h.Repository.Database.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM agent_system_update_tasks WHERE id=?)", r.PathValue("id")).Scan(&system); err != nil {
		httpapi.Error(w, 500, "internal_error", err.Error())
		return
	}
	if system {
		items, total, err := h.Repository.ListAgentSystemAttempts(r.Context(), r.PathValue("id"), page, size)
		if err != nil {
			httpapi.Error(w, 409, "agent_update_conflict", err.Error())
			return
		}
		httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
		return
	}
	items, total, err := h.Repository.ListAgentUpdateAttempts(r.Context(), r.PathValue("id"), page, size)
	if err != nil {
		httpapi.Error(w, 409, "agent_update_conflict", "无法读取升级尝试记录")
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (h *AgentUpdateHandler) recover(w http.ResponseWriter, r *http.Request) {
	var input AgentRecoveryConfirmation
	if httpapi.Decode(w, r, &input) != nil || input.NodeID == "" || input.CommandID == "" || input.Revision < 1 || input.Generation < 1 {
		httpapi.Error(w, 400, "invalid_request", "请确认原任务、节点、配置版本和尝试编号")
		return
	}
	task, err := h.Repository.RetryAgentRecovery(r.Context(), h.Catalogue, r.PathValue("id"), auth.Actor(r.Context()).Username, r.RemoteAddr, input)
	if err != nil {
		httpapi.Error(w, 409, "agent_recovery_conflict", "恢复能力、原任务或运行配置不符合恢复条件")
		return
	}
	httpapi.JSON(w, 202, task)
}
func (h *AgentUpdateHandler) list(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "分页参数无效")
		return
	}
	items, total, err := h.Repository.ListAllAgentUpdates(r.Context(), page, size)
	if err != nil {
		httpapi.Error(w, 500, "internal_error", "暂时无法读取 Agent 升级任务")
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

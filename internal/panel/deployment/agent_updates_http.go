package deployment

import (
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/release"
)

type AgentUpdateHandler struct {
	Repository *Repository
	Catalogue  ReleaseAccess
	Keys       map[string]release.TrustedKey
	Auth       *auth.Handler
}

func (h *AgentUpdateHandler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/agent-updates", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("POST /api/v1/agent-updates", h.Auth.Require(http.HandlerFunc(h.queue), true))
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
	items, total, err := h.Repository.ListAgentUpdates(r.Context(), page, size)
	if err != nil {
		httpapi.Error(w, 500, "internal_error", "暂时无法读取 Agent 升级任务")
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *AgentUpdateHandler) queue(w http.ResponseWriter, r *http.Request) {
	var input struct {
		NodeID        string `json:"nodeId"`
		ReleaseID     string `json:"releaseId"`
		Revision      int64  `json:"revision"`
		ConfirmNodeID string `json:"confirmNodeId"`
	}
	if httpapi.Decode(w, r, &input) != nil || input.NodeID == "" || input.NodeID != input.ConfirmNodeID {
		httpapi.Error(w, 400, "invalid_request", "请确认目标节点和当前配置版本")
		return
	}
	task, err := h.Repository.QueueAgentUpdate(r.Context(), h.Catalogue, h.Keys, input.NodeID, input.ReleaseID, auth.Actor(r.Context()).Username, r.RemoteAddr, input.Revision)
	if err != nil {
		httpapi.Error(w, 409, "agent_update_conflict", "节点能力、已发布配置、版本信任或运行中操作存在冲突")
		return
	}
	httpapi.JSON(w, 202, task)
}

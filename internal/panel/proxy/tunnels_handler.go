package proxy

import (
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"strconv"
	"strings"

	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) registerTunnels(mux *http.ServeMux) {
	for pattern, handler := range map[string]http.HandlerFunc{
		"POST /api/v1/tunnels/plan":                    h.planTunnels,
		"POST /api/v1/tunnels/publish":                 h.publishTunnels,
		"GET /api/v1/tunnels":                          h.listTunnels,
		"POST /api/v1/tunnels":                         h.saveTunnel,
		"PUT /api/v1/tunnels/{id}":                     h.saveTunnel,
		"DELETE /api/v1/tunnels/{id}":                  h.removeTunnel,
		"POST /api/v1/tunnels/{id}/rotate-credentials": h.rotateTunnel,
		"GET /api/v1/tunnel-rules":                     h.listTunnelRules,
		"POST /api/v1/tunnel-rules":                    h.saveTunnelRule,
		"PUT /api/v1/tunnel-rules/{id}":                h.saveTunnelRule,
		"DELETE /api/v1/tunnel-rules/{id}":             h.removeTunnelRule,
		"GET /api/v1/nodes/{id}/ports/check":           h.checkNodePort,
		"POST /api/v1/nodes/{id}/ports/allocate":       h.allocateNodePorts,
	} {
		mux.Handle(pattern, h.Auth.Require(handler, !strings.HasPrefix(pattern, "GET ")))
	}
}
func (h *Handler) failTunnel(w http.ResponseWriter, err error) {
	var e *tunnelError
	if errors.As(err, &e) {
		httpapi.ErrorDetails(w, e.Status, e.Code, e.Message, e.Details)
		return
	}
	if errors.Is(err, ErrNotFound) {
		httpapi.Error(w, 404, "not_found", "对象不存在")
		return
	}
	h.internal(w, err)
}

func (h *Handler) planTunnels(w http.ResponseWriter, r *http.Request) {
	var input PublishSelection
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	plan, err := h.Service.PlanTunnels(r.Context(), input)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	httpapi.JSON(w, 200, plan)
}
func (h *Handler) publishTunnels(w http.ResponseWriter, r *http.Request) {
	var input PublishSelection
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	result, err := h.Service.PublishTunnels(r.Context(), input, auth.Actor(r.Context()).Username, h.Users)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	if h.tunnelAudit(w, r, "发布隧道与规则", result.BatchID) {
		httpapi.JSON(w, 200, result)
	}
}
func (h *Handler) listTunnels(w http.ResponseWriter, r *http.Request) {
	h.Service.canvasMu.Lock()
	defer h.Service.canvasMu.Unlock()
	items, err := h.Service.Repository.Tunnels(r.Context())
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (h *Handler) saveTunnel(w http.ResponseWriter, r *http.Request) {
	var input TunnelInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	item, err := h.Service.SaveTunnel(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	status, action := 200, "修改隧道"
	if r.Method == "POST" {
		status, action = 201, "创建隧道"
	}
	if h.tunnelAudit(w, r, action, item.ID) {
		httpapi.JSON(w, status, item)
	}
}
func (h *Handler) removeTunnel(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.DeleteTunnel(r.Context(), r.PathValue("id")); err != nil {
		h.failTunnel(w, err)
		return
	}
	if h.tunnelAudit(w, r, "删除隧道", r.PathValue("id")) {
		w.WriteHeader(204)
	}
}
func (h *Handler) rotateTunnel(w http.ResponseWriter, r *http.Request) {
	var input struct {
		UpdatedAt string `json:"updatedAt"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	item, err := h.Service.RotateTunnelCredentials(r.Context(), r.PathValue("id"), input.UpdatedAt)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	if h.tunnelAudit(w, r, "轮换隧道凭据", item.ID) {
		httpapi.JSON(w, 200, item)
	}
}
func (h *Handler) listTunnelRules(w http.ResponseWriter, r *http.Request) {
	h.Service.canvasMu.Lock()
	defer h.Service.canvasMu.Unlock()
	items, err := h.Service.Repository.TunnelRules(r.Context(), r.URL.Query().Get("tunnelId"), r.URL.Query().Get("nodeId"))
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (h *Handler) saveTunnelRule(w http.ResponseWriter, r *http.Request) {
	var input TunnelRuleInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	item, err := h.Service.SaveTunnelRule(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	status, action := 200, "修改隧道规则"
	if r.Method == "POST" {
		status, action = 201, "创建隧道规则"
	}
	if h.tunnelAudit(w, r, action, item.ID) {
		httpapi.JSON(w, status, item)
	}
}
func (h *Handler) removeTunnelRule(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.DeleteTunnelRule(r.Context(), r.PathValue("id")); err != nil {
		h.failTunnel(w, err)
		return
	}
	if h.tunnelAudit(w, r, "删除隧道规则", r.PathValue("id")) {
		w.WriteHeader(204)
	}
}
func (h *Handler) checkNodePort(w http.ResponseWriter, r *http.Request) {
	port, _ := strconv.Atoi(r.URL.Query().Get("port"))
	check, err := h.Service.Repository.CheckPort(r.Context(), r.PathValue("id"), port, nil)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	httpapi.JSON(w, 200, check)
}
func (h *Handler) allocateNodePorts(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Count *int `json:"count"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	count := 1
	if input.Count != nil {
		count = *input.Count
	}
	ports, err := h.Service.Repository.AllocatePorts(r.Context(), r.PathValue("id"), count, nil)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	if h.tunnelAudit(w, r, "建议分配端口", r.PathValue("id")) {
		httpapi.JSON(w, 200, map[string]any{"ports": ports})
	}
}

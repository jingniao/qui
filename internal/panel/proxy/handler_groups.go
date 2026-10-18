package proxy

import (
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) RegisterGroups(mux *http.ServeMux) {
	for pattern, handler := range map[string]http.HandlerFunc{"GET /api/v1/outbound-groups": h.groups, "POST /api/v1/outbound-groups": h.saveGroup, "PUT /api/v1/outbound-groups/{id}": h.saveGroup, "DELETE /api/v1/outbound-groups/{id}": h.deleteGroup, "GET /api/v1/proxy-chains": h.chains, "GET /api/v1/proxy-chains/{id}": h.chain, "POST /api/v1/proxy-chains": h.saveChain, "PUT /api/v1/proxy-chains/{id}": h.saveChain, "DELETE /api/v1/proxy-chains/{id}": h.deleteChain} {
		mux.Handle(pattern, h.Auth.Require(handler, true))
	}
}
func (h *Handler) groups(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Service.Repository.Groups(r.Context(), r.URL.Query().Get("nodeId"), page, size)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) saveGroup(w http.ResponseWriter, r *http.Request) {
	var input GroupInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	group, err := h.Service.SaveGroup(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "保存出口组", group.Name, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	httpapi.JSON(w, status, group)
}
func (h *Handler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.DeleteGroup(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "删除出口组", r.PathValue("id"), "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) chains(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Service.Repository.Chains(r.Context(), r.URL.Query().Get("nodeId"), page, size)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) chain(w http.ResponseWriter, r *http.Request) {
	chain, err := h.Service.Repository.Chain(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, chain)
}
func (h *Handler) saveChain(w http.ResponseWriter, r *http.Request) {
	var input ChainInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	chain, err := h.Service.SaveChain(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "保存代理链", chain.Name, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	httpapi.JSON(w, status, chain)
}
func (h *Handler) deleteChain(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.DeleteChain(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "删除代理链", r.PathValue("id"), "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}

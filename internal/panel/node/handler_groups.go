package node

import (
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) RegisterGroups(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/node-groups", h.Auth.Require(http.HandlerFunc(h.listGroups), true))
	mux.Handle("POST /api/v1/node-groups", h.Auth.Require(http.HandlerFunc(h.saveGroup), true))
	mux.Handle("PUT /api/v1/node-groups/{id}", h.Auth.Require(http.HandlerFunc(h.saveGroup), true))
	mux.Handle("DELETE /api/v1/node-groups/{id}", h.Auth.Require(http.HandlerFunc(h.deleteGroup), true))
}
func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.Groups(r.Context(), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) saveGroup(w http.ResponseWriter, r *http.Request) {
	var input GroupInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	group, err := h.Repository.SaveGroup(r.Context(), r.PathValue("id"), input)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.fail(w, err)
		} else {
			httpapi.Error(w, 400, "invalid_request", err.Error())
		}
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "保存节点组", group.Name, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	httpapi.JSON(w, status, group)
}
func (h *Handler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.Repository.DeleteGroup(r.Context(), id); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "删除节点组", id, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

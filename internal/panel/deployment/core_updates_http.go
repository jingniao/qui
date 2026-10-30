package deployment

import (
	"database/sql"
	"errors"
	"net/http"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

type CoreUpdateHandler struct {
	Repository *Repository
	Audit      ReleaseAccess
	Auth       *auth.Handler
}

func (h *CoreUpdateHandler) Register(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/nodes/{id}/cores/{component}/remove", h.Auth.Require(http.HandlerFunc(h.remove), true))
	mux.Handle("GET /api/v1/nodes/{id}/cores/{component}/removals", h.Auth.Require(http.HandlerFunc(h.removals), true))
	mux.Handle("GET /api/v1/core-updates", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("POST /api/v1/core-updates", h.Auth.Require(http.HandlerFunc(h.queue), true))
	mux.Handle("POST /api/v1/core-updates/{id}/clear", h.Auth.Require(http.HandlerFunc(h.clear), true))
}

func (h *CoreUpdateHandler) list(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.ListCoreUpdates(r.Context(), page, size)
	if err != nil {
		httpapi.Error(w, 500, "internal_error", "Core updates unavailable")
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (h *CoreUpdateHandler) queue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		NodeID        string `json:"nodeId"`
		ConfirmNodeID string `json:"confirmNodeId"`
		Revision      int64  `json:"revision"`
		Component     string `json:"component"`
		Version       string `json:"version"`
		URL           string `json:"url"`
		SHA256        string `json:"sha256"`
	}
	if httpapi.Decode(w, r, &input) != nil || input.NodeID == "" || input.NodeID != input.ConfirmNodeID {
		httpapi.Error(w, 400, "invalid_request", "Confirm target node and current revision")
		return
	}
	task, err := h.Repository.QueueCoreUpdate(r.Context(), h.Audit, input.NodeID, auth.Actor(r.Context()).Username, r.RemoteAddr, input.Revision, CoreUpdateInput{Component: input.Component, Version: input.Version, URL: input.URL, SHA256: input.SHA256})
	if err != nil {
		httpapi.Error(w, 409, "core_update_conflict", "Node, revision, core, version, official asset URL or digest conflicts with an active operation")
		return
	}
	httpapi.JSON(w, 202, task)
}

func (h *CoreUpdateHandler) clear(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	task, err := h.Repository.ClearCoreUpdate(r.Context(), h.Audit, r.PathValue("id"), auth.Actor(r.Context()).Username, r.RemoteAddr)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Error(w, 404, "not_found", "Core update task not found")
		return
	}
	if err != nil {
		httpapi.Error(w, 500, "internal_error", "Core update task could not be cleared")
		return
	}
	httpapi.JSON(w, 200, task)
}

package system

import (
	"net/http"

	"proxycontrol/internal/platform/httpapi"
)

// ReleaseHandler lists the signed Agent releases the panel has imported. Since
// v1.6.1 releases only enter the catalogue through the GitHub import, which keeps
// the newest one per platform.
type ReleaseHandler struct {
	Repository *Repository
	Auth       interface {
		Require(http.Handler, bool) http.Handler
	}
}

func (h *ReleaseHandler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/releases", h.Auth.Require(http.HandlerFunc(h.list), true))
}
func (h *ReleaseHandler) list(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.ListReleases(r.Context(), page, size)
	if err != nil {
		httpapi.Error(w, 500, "internal_error", "Release catalogue unavailable")
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

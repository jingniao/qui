package node

import (
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) choices(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Repository.Choices(r.Context(), r.URL.Query().Get("keyword"), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

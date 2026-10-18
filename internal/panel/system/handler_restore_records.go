package system

import (
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) restoreRecords(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	if h.Backups == nil {
		httpapi.Error(w, 503, "unavailable", "Restore progress unavailable")
		return
	}
	records, total, err := h.Backups.RestoreRecords(r.Context(), page, size)
	if err != nil {
		httpapi.Error(w, 409, "restore_progress_invalid", "Restore progress requires verification")
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": records, "total": total, "page": page, "pageSize": size})
}

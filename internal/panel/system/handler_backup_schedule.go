package system

import (
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) backupSchedule(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	settings, err := h.Repository.BackupSchedule(r.Context())
	if err != nil {
		h.backupFail(w, err)
		return
	}
	httpapi.JSON(w, 200, settings)
}
func (h *Handler) saveBackupSchedule(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	var input BackupScheduleInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid backup schedule request")
		return
	}
	settings, changed, err := h.Backups.SaveSchedule(r.Context(), input, h.ActorName(r.Context()), r.RemoteAddr)
	input.Password = ""
	if err != nil {
		h.backupFail(w, err)
		return
	}
	if changed && h.BackupChanged != nil {
		h.BackupChanged()
	}
	httpapi.JSON(w, 200, settings)
}
func (h *Handler) backupRuns(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.BackupRuns(r.Context(), page, size)
	if err != nil {
		h.backupFail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

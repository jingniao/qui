package system

import (
	"context"
	"net/http"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/httpapi"
	"time"
)

func (h *Handler) executeRestore(w http.ResponseWriter, r *http.Request) {
	if h.Restore == nil || h.Restore.Reserve == nil || h.Restore.Accept == nil {
		httpapi.Error(w, 503, "restore_unavailable", "Managed restore is not enabled for this service")
		return
	}
	var input struct {
		Password        string `json:"password"`
		ArchiveSHA256   string `json:"archiveSha256"`
		ConfirmBackupID string `json:"confirmBackupId"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil || input.ConfirmBackupID != r.PathValue("id") || len(input.ArchiveSHA256) != 64 {
		httpapi.Error(w, 400, "invalid_request", "Confirm the selected backup and validated archive")
		return
	}
	release, err := h.Restore.Reserve()
	if err != nil {
		httpapi.Error(w, 409, "restore_busy", "Another restore is already being prepared")
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	prepared, err := h.Backups.PrepareOnlineRestore(ctx, r.PathValue("id"), input.Password, input.ArchiveSHA256, h.Restore.Target)
	if err != nil {
		input.Password = ""
		h.backupFail(w, err)
		return
	}
	password := []byte(input.Password)
	input.Password = ""
	record, dispatch, err := h.Restore.Accept(ctx, prepared, password, h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		clear(password)
		archive.DiscardPreparedRestore(context.Background(), prepared)
		httpapi.Error(w, 409, "restore_rejected", "Restore preparation could not be accepted")
		return
	}
	httpapi.JSON(w, 202, record)
	http.NewResponseController(w).Flush()
	dispatch()
}

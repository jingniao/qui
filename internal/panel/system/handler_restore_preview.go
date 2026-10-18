package system

import (
	"context"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
	"time"
)

func (h *Handler) restorePreview(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid restore preview request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := h.Backups.RestorePreview(ctx, r.PathValue("id"), body.Password)
	body.Password = ""
	if err != nil {
		h.backupFail(w, err)
		return
	}
	if h.Restore != nil && result.SameKeyMaterial && result.SameNodeCA && result.Snapshot.ActiveOwners == 1 && result.Current.ActiveOwners == 1 && result.Current.PendingOperations == 0 && result.Current.BackupFileOperations == 0 {
		result.ExecutionAvailable = true
		issues := result.Issues[:0]
		for _, issue := range result.Issues {
			if issue != "restore_executor_pending" {
				issues = append(issues, issue)
			}
		}
		result.Issues = issues
	}
	httpapi.JSON(w, 200, result)
}

package system

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/httpapi"
	"time"
)

func (h *Handler) backupService(w http.ResponseWriter) bool {
	if h.Backups == nil || h.ActorName == nil {
		httpapi.Error(w, 503, "backup_unavailable", "Backup service unavailable")
		return false
	}
	return true
}

func (h *Handler) listBackups(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.Backups(r.Context(), page, size)
	if err != nil {
		h.backupFail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (h *Handler) createBackup(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid backup request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	item, err := h.Backups.Create(ctx, h.ActorName(r.Context()), r.RemoteAddr, body.Password)
	body.Password = ""
	if err != nil {
		h.backupFail(w, err)
		return
	}
	httpapi.JSON(w, 201, item)
}

func (h *Handler) verifyBackup(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid backup request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	item, err := h.Backups.Verify(ctx, r.PathValue("id"), h.ActorName(r.Context()), r.RemoteAddr, body.Password)
	body.Password = ""
	if err != nil {
		h.backupFail(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}

func (h *Handler) downloadBackup(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	file, item, err := h.Backups.Download(r.Context(), r.PathValue("id"), h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		h.backupFail(w, err)
		return
	}
	defer file.Close()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="proxy-control-`+item.ID+`.pcb"`)
	// The immutable encrypted artifact has been checked against its catalog;
	// range requests support resuming a large download without returning secrets.
	http.ServeContent(w, r, "proxy-control-"+item.ID+".pcb", time.Time{}, file)
}

func (h *Handler) deleteBackup(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	if err := h.Backups.Delete(r.Context(), r.PathValue("id"), h.ActorName(r.Context()), r.RemoteAddr); err != nil {
		h.backupFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) backupFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBackupImportConflict):
		httpapi.Error(w, 409, "backup_import_conflict", "备份编号已存在但文件内容不同，请核对来源。")
	case errors.Is(err, archive.ErrReceive):
		httpapi.Error(w, 400, "invalid_backup_upload", "请上传有效的加密备份和密码，文件不得超过服务端限制。")
	case errors.Is(err, ErrBackupRecovery):
		httpapi.Error(w, 409, "backup_recovery_required", "备份文件与登记状态不一致，请检查并恢复匹配的备份目录。")
	case errors.Is(err, ErrBackupScheduleConflict):
		httpapi.Error(w, 409, "backup_schedule_conflict", "自动备份策略已变化，请刷新后再提交。")
	case errors.Is(err, ErrBackupScheduleInvalid):
		httpapi.Error(w, 400, "invalid_backup_schedule", "自动备份策略或加密密码不可用，请检查设置。")
	case errors.Is(err, context.DeadlineExceeded):
		httpapi.Error(w, 503, "backup_timeout", "备份操作超时，请检查服务端存储状态。")
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Error(w, 404, "not_found", "Backup not found")
	case errors.Is(err, archive.ErrPassword):
		httpapi.Error(w, 400, "invalid_backup_password", "备份密码须包含 12～128 个字符。")
	case errors.Is(err, archive.ErrArchive):
		httpapi.Error(w, 400, "backup_verification_failed", "备份校验失败，请核对密码和文件。")
	case errors.Is(err, ErrBackupBusy):
		httpapi.Error(w, 409, "backup_busy", "有其他备份操作正在执行，请稍后重试。")
	case errors.Is(err, ErrBackupFile):
		httpapi.Error(w, 409, "backup_file_unavailable", "备份文件缺失或校验和不一致。")
	default:
		if h.Logger != nil {
			h.Logger.Error("backup operation failed")
		}
		httpapi.Error(w, 500, "internal_error", "Backup operation failed")
	}
}

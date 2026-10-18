package system

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

type Handler struct {
	Repository *Repository
	Auth       interface {
		Require(http.Handler, bool) http.Handler
	}
	ActorName        func(context.Context) string
	ActorID          func(context.Context) string
	Logger           *slog.Logger
	RetentionChanged func()
	BaseChanged      func()
	Backups          *BackupService
	BackupChanged    func()
	Restore          *OnlineRestore
	Runtime          RuntimeProvider
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/system/runtime", h.Auth.Require(http.HandlerFunc(h.runtime), true))
	mux.Handle("GET /api/v1/system/runtime-policy", h.Auth.Require(http.HandlerFunc(h.runtimePolicy), true))
	mux.Handle("PUT /api/v1/system/runtime-policy", h.Auth.Require(http.HandlerFunc(h.saveRuntimePolicy), true))
	mux.Handle("GET /api/v1/system/backup-schedule", h.Auth.Require(http.HandlerFunc(h.backupSchedule), true))
	mux.Handle("PUT /api/v1/system/backup-schedule", h.Auth.Require(http.HandlerFunc(h.saveBackupSchedule), true))
	mux.Handle("POST /api/v1/system/backup-retention-preview", h.Auth.Require(http.HandlerFunc(h.backupRetentionPreview), true))
	mux.Handle("GET /api/v1/backup-runs", h.Auth.Require(http.HandlerFunc(h.backupRuns), true))
	mux.Handle("GET /api/v1/backups", h.Auth.Require(http.HandlerFunc(h.listBackups), true))
	mux.Handle("POST /api/v1/backups", h.Auth.Require(http.HandlerFunc(h.createBackup), true))
	mux.Handle("POST /api/v1/backups/import", h.Auth.Require(http.HandlerFunc(h.importBackup), true))
	mux.Handle("POST /api/v1/backups/{id}/verify", h.Auth.Require(http.HandlerFunc(h.verifyBackup), true))
	mux.Handle("POST /api/v1/backups/{id}/restore-preview", h.Auth.Require(http.HandlerFunc(h.restorePreview), true))
	mux.Handle("POST /api/v1/backups/{id}/restore", h.Auth.Require(http.HandlerFunc(h.executeRestore), true))
	mux.Handle("GET /api/v1/restore-records", h.Auth.Require(http.HandlerFunc(h.restoreRecords), true))
	mux.Handle("GET /api/v1/backups/{id}/download", h.Auth.Require(http.HandlerFunc(h.downloadBackup), true))
	mux.Handle("DELETE /api/v1/backups/{id}", h.Auth.Require(http.HandlerFunc(h.deleteBackup), true))
	mux.Handle("GET /api/v1/system/base", h.Auth.Require(http.HandlerFunc(h.base), true))
	mux.Handle("PUT /api/v1/system/base", h.Auth.Require(http.HandlerFunc(h.saveBase), true))
	mux.Handle("GET /api/v1/system/access-retention", h.Auth.Require(http.HandlerFunc(h.retention), true))
	mux.Handle("PUT /api/v1/system/access-retention", h.Auth.Require(http.HandlerFunc(h.saveRetention), true))
	mux.Handle("GET /api/v1/system/storage-thresholds", h.Auth.Require(http.HandlerFunc(h.storageThresholds), true))
	mux.Handle("PUT /api/v1/system/storage-thresholds", h.Auth.Require(http.HandlerFunc(h.saveStorageThresholds), true))
}
func (h *Handler) storageThresholds(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Repository.ReadStorageThresholds(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, settings)
}
func (h *Handler) saveStorageThresholds(w http.ResponseWriter, r *http.Request) {
	if h.ActorName == nil {
		httpapi.Error(w, 500, "internal_error", "Settings audit identity unavailable")
		return
	}
	var input StorageThresholdInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	settings, err := h.Repository.SaveStorageThresholds(r.Context(), input, h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, settings)
}
func (h *Handler) retention(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Repository.ReadRetention(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, settings)
}
func (h *Handler) saveRetention(w http.ResponseWriter, r *http.Request) {
	if h.ActorName == nil {
		httpapi.Error(w, 500, "internal_error", "Settings audit identity unavailable")
		return
	}
	var input RetentionInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	settings, err := h.Repository.SaveRetention(r.Context(), input, h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	if h.RetentionChanged != nil {
		h.RetentionChanged()
	}
	httpapi.JSON(w, 200, settings)
}
func (h *Handler) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrBaseConflict) {
		httpapi.Error(w, 409, "settings_conflict", err.Error())
		return
	}
	if errors.Is(err, ErrBaseInvalid) || errors.Is(err, ErrRuntimeInvalid) {
		httpapi.Error(w, 400, "invalid_settings", err.Error())
		return
	}
	if errors.Is(err, ErrRetentionConflict) {
		httpapi.Error(w, 409, "settings_conflict", err.Error())
		return
	}
	if errors.Is(err, ErrRetentionInvalid) {
		httpapi.Error(w, 400, "invalid_settings", err.Error())
		return
	}
	if errors.Is(err, ErrStorageThresholdConflict) {
		httpapi.Error(w, 409, "settings_conflict", err.Error())
		return
	}
	if errors.Is(err, ErrStorageThresholdInvalid) {
		httpapi.Error(w, 400, "invalid_settings", err.Error())
		return
	}
	if h.Logger != nil {
		h.Logger.Error("retention settings operation failed")
	}
	httpapi.Error(w, 500, "internal_error", "Retention settings unavailable")
}

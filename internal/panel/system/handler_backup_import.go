package system

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/httpapi"
	"time"
)

func (h *Handler) importBackup(w http.ResponseWriter, r *http.Request) {
	if !h.backupService(w) {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" || r.URL.RawQuery != "" || r.ContentLength > archive.MaxArchiveBytes+(64<<10) {
		h.backupFail(w, archive.ErrReceive)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, archive.MaxArchiveBytes+(64<<10))
	defer r.Body.Close()
	// This endpoint alone permits a bounded longer upload; normal JSON API
	// limits and the server's default read timeout remain in place.
	controller := http.NewResponseController(w)
	if err := controller.SetReadDeadline(time.Now().Add(5 * time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.backupFail(w, err)
		return
	}
	defer controller.SetReadDeadline(time.Time{})
	multipart, err := r.MultipartReader()
	if err != nil {
		h.backupFail(w, archive.ErrReceive)
		return
	}
	passwordPart, err := multipart.NextPart()
	if err != nil || passwordPart.FormName() != "password" || passwordPart.FileName() != "" {
		h.backupFail(w, archive.ErrReceive)
		return
	}
	passwordData, err := io.ReadAll(io.LimitReader(passwordPart, 513))
	if err != nil || len(passwordData) > 512 {
		clear(passwordData)
		h.backupFail(w, archive.ErrReceive)
		return
	}
	passwordPart.Close()
	password := string(passwordData)
	clear(passwordData)
	if err := archive.ValidatePassword(password); err != nil {
		h.backupFail(w, err)
		return
	}
	filePart, err := multipart.NextPart()
	if err != nil || filePart.FormName() != "archive" || filePart.FileName() == "" {
		h.backupFail(w, archive.ErrReceive)
		return
	}
	defer filePart.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	item, created, err := h.Backups.Import(ctx, h.ActorName(r.Context()), r.RemoteAddr, password, filePart, func() error {
		_, err := multipart.NextPart()
		if err == io.EOF {
			return nil
		}
		return archive.ErrReceive
	})
	password = ""
	if err != nil {
		h.backupFail(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpapi.JSON(w, status, item)
}

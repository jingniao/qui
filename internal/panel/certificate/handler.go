package certificate

import (
	"context"
	"errors"
	"net/http"

	"proxycontrol/internal/platform/httpapi"
)

// Handler serves the certificate API. Every route is Owner-only and every write
// requires CSRF, which is what Require(handler, true) enforces; no route lives on
// the portal or subscription path, so neither session kind can reach them.
type Handler struct {
	Service *Service
	Auth    interface {
		Require(http.Handler, bool) http.Handler
	}
	Actor func(context.Context) string
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/system/certificate-settings", h.Auth.Require(http.HandlerFunc(h.settings), true))
	mux.Handle("PUT /api/v1/system/certificate-settings", h.Auth.Require(http.HandlerFunc(h.saveSettings), true))
	mux.Handle("GET /api/v1/certificates", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("POST /api/v1/certificates", h.Auth.Require(http.HandlerFunc(h.create), true))
	mux.Handle("POST /api/v1/certificates/{id}/renew", h.Auth.Require(http.HandlerFunc(h.renew), true))
	mux.Handle("DELETE /api/v1/certificates/{id}", h.Auth.Require(http.HandlerFunc(h.remove), true))
}

// actor is the signed-in Owner's name, which every audit row needs.
func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.Actor == nil {
		httpapi.Error(w, 500, "internal_error", "Audit identity unavailable")
		return "", false
	}
	return h.Actor(r.Context()), true
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	view, err := h.Service.SettingsView(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, view)
}

func (h *Handler) saveSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var input SettingsInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	view, _, err := h.Service.SaveSettings(r.Context(), input, actor, r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, view)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	views, err := h.Service.List(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": views})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var input CreateInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	view, err := h.Service.Create(r.Context(), input, actor, r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, view)
}

func (h *Handler) renew(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	view, err := h.Service.Renew(r.Context(), r.PathValue("id"), actor, r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, view)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.Service.Delete(r.Context(), r.PathValue("id"), actor, r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fail maps the module's errors onto the documented codes. Upstream text never
// reaches the response: every message is either the client's own validation
// complaint or a fixed Chinese sentence.
func (h *Handler) fail(w http.ResponseWriter, err error) {
	var inUse *InUseError
	switch {
	case errors.As(err, &inUse):
		httpapi.ErrorDetails(w, 409, "certificate_in_use", "证书仍被入站引用，不能删除", map[string]any{"inbounds": inUse.Inbounds})
	case errors.Is(err, ErrSettingsConflict):
		httpapi.Error(w, 409, "settings_conflict", err.Error())
	case errors.Is(err, ErrTokenInvalid):
		httpapi.Error(w, 400, "cloudflare_token_invalid", "Cloudflare API Token 无效或权限不足")
	case errors.Is(err, ErrTokenUnverified):
		httpapi.Error(w, 502, "cloudflare_api_failed", "无法验证 Cloudflare API Token，请稍后重试")
	case errors.Is(err, ErrSettingsMissing):
		httpapi.Error(w, 400, "certificate_settings_missing", "请先在系统设置里填写邮箱并保存 Cloudflare API Token")
	case errors.Is(err, ErrDomains), errors.Is(err, ErrSettingsInvalid), errors.Is(err, ErrNodeUnknown):
		httpapi.Error(w, 400, "invalid_request", err.Error())
	case errors.Is(err, ErrNotFound):
		httpapi.Error(w, 404, "not_found", "证书不存在")
	default:
		httpapi.Error(w, 500, "internal_error", "证书操作失败")
	}
}

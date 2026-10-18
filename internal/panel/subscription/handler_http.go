package subscription

import (
	"context"
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/httpapi"
)

type Handler struct {
	Service  *Service
	Auth     *auth.Handler
	Audit    auth.Audit
	Settings interface {
		DefaultSubscriptionFormat(context.Context) (string, error)
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /sub/{token}", h.content)
	mux.Handle("GET /api/v1/subscriptions", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("POST /api/v1/users/{id}/subscription-token", h.Auth.Require(http.HandlerFunc(h.rotate), true))
	mux.Handle("DELETE /api/v1/users/{id}/subscription-token", h.Auth.Require(http.HandlerFunc(h.revoke), true))
	mux.Handle("GET /api/v1/users/{id}/subscription-preview", h.Auth.Require(http.HandlerFunc(h.preview), true))
}
func (h *Handler) content(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" && h.Settings != nil {
		configured, err := h.Settings.DefaultSubscriptionFormat(r.Context())
		if err != nil {
			httpapi.Error(w, 503, "subscription_unavailable", "Subscription settings unavailable")
			return
		}
		format = configured
	}
	content, _, err := h.Service.Content(r.Context(), r.PathValue("token"), format)
	if err != nil {
		if errors.Is(err, ErrToken) || errors.Is(err, user.ErrInactive) || errors.Is(err, user.ErrNotFound) || errors.Is(err, user.ErrCredential) {
			httpapi.Error(w, 403, "subscription_unavailable", "Subscription unavailable")
		} else {
			// The token holder is unauthenticated; never echo internal error text
			// (published-state identity, key or material details) to them.
			h.Auth.Logger.Error("subscription content failed", "error", err)
			httpapi.Error(w, 400, "invalid_subscription", "Subscription unavailable")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if format == "sing-box" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.Write(content)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Service.Repository.List(r.Context(), page, size)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) rotate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID string `json:"deviceId"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	token, plaintext, err := h.Service.Rotate(r.Context(), r.PathValue("id"), body.DeviceID)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "重置订阅 Token", token.UserID, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 201, map[string]any{"subscription": token, "token": plaintext, "path": "/sub/" + plaintext})
}
func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, err := h.Service.Users.Get(r.Context(), userID); err != nil {
		httpapi.Error(w, 404, "not_found", "User not found")
		return
	}
	if err := h.Service.Repository.Revoke(r.Context(), userID); err != nil {
		h.internal(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "撤销订阅 Token", userID, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	endpoints, err := h.Service.Endpoints(r.Context(), r.PathValue("id"), r.URL.Query().Get("deviceId"))
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": endpoints, "total": len(endpoints)})
}
func (h *Handler) internal(w http.ResponseWriter, err error) {
	h.Auth.Logger.Error("subscription operation failed", "error", err)
	httpapi.Error(w, 500, "internal_error", "Subscription operation failed")
}

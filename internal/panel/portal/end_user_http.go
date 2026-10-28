package portal

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !auth.SameOrigin(r) {
		httpapi.Error(w, 403, "csrf_failed", "Cross-origin login denied")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	token, view, err := h.Service.Login(r.Context(), body.Username, body.Password, h.Auth.ClientIP(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), view.CurrentUser.Username, "用户登录", view.CurrentUser.ID, "成功", h.Auth.ClientIP(r)); err != nil {
		_ = h.Repository.DeleteSession(r.Context(), auth.HashToken(token))
		h.fail(w, err)
		return
	}
	h.setCookie(w, token, view.ExpiresAt)
	httpapi.JSON(w, 200, view)
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) { httpapi.JSON(w, 200, current(r)) }
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	httpapi.JSON(w, 200, current(r).CurrentUser)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Repository.DeleteSession(r.Context(), auth.HashToken(h.token(r))); err != nil {
		h.fail(w, err)
		return
	}
	h.setCookie(w, "", time.Time{})
	w.WriteHeader(204)
}

func (h *Handler) password(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	a, err := h.Repository.Account(r.Context(), current(r).CurrentUser.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if !auth.VerifyPassword(a.PasswordHash, body.CurrentPassword) {
		h.fail(w, auth.ErrCredentials)
		return
	}
	hash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if err := h.Repository.ChangePassword(r.Context(), a.ID, a.PasswordHash, hash, auth.HashToken(h.token(r))); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), a.Username, "修改用户登录密码", a.ID, "成功", h.Auth.ClientIP(r)); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) traffic(w http.ResponseWriter, r *http.Request) {
	rangeName := r.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = "7d"
	}
	now := time.Now().UTC()
	granularity, days := "daily", 7
	switch rangeName {
	case "7d":
	case "30d":
		days = 30
	case "24h":
		granularity = "hourly"
	default:
		httpapi.Error(w, 400, "invalid_request", "Invalid traffic range")
		return
	}
	from := now.Truncate(24*time.Hour).AddDate(0, 0, 1-days)
	if granularity == "hourly" {
		from = now.Truncate(time.Hour).Add(-23 * time.Hour)
	}
	items, _, err := h.Service.Traffic.AccountTraffic(r.Context(), access.Filter{UserID: current(r).CurrentUser.ID, From: from, To: now}, granularity, 1, 32)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"range": rangeName, "granularity": granularity, "items": items})
}

func (h *Handler) nodes(w http.ResponseWriter, r *http.Request) {
	items, err := h.Service.GrantedNodes(r.Context(), current(r).CurrentUser.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}

func (h *Handler) subscription(w http.ResponseWriter, r *http.Request) {
	view, err := h.subscriptionView(r.Context(), current(r).CurrentUser.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, view)
}

func (h *Handler) subscriptionView(ctx context.Context, id string) (map[string]any, error) {
	token, err := h.Service.Subscriptions.Repository.Link(ctx, id)
	switch {
	case errors.Is(err, subscription.ErrNotRecoverable):
		return map[string]any{"state": "not_recoverable"}, nil
	case errors.Is(err, subscription.ErrToken):
		return map[string]any{"state": "missing"}, nil
	case err != nil:
		return nil, err
	}
	return h.linkView(token), nil
}

func (h *Handler) subscriptionLink(w http.ResponseWriter, token string, status int) {
	httpapi.JSON(w, status, h.linkView(token))
}

func (h *Handler) linkView(token string) map[string]any {
	path := "/sub/" + token
	formats := []map[string]string{}
	registry := h.Service.Subscriptions.Formats
	if registry == nil {
		registry = subscription.DefaultFormats()
	}
	for _, info := range registry.Info() {
		formats = append(formats, map[string]string{"id": info.ID, "url": path + "?format=" + url.QueryEscape(info.ID)})
	}
	return map[string]any{"state": "ready", "path": path, "formats": formats}
}

func (h *Handler) resetSubscription(w http.ResponseWriter, r *http.Request) {
	// No body field selects an account: even an injected ID cannot change the
	// principal resolved by RequirePortal.
	p := current(r).CurrentUser
	_, token, err := h.Service.Subscriptions.Rotate(r.Context(), p.ID, "")
	if errors.Is(err, user.ErrInactive) {
		httpapi.Error(w, 403, "subscription_unavailable", "Subscription unavailable")
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), p.Username, "重置订阅 Token", p.ID, "成功", h.Auth.ClientIP(r)); err != nil {
		h.fail(w, err)
		return
	}
	h.subscriptionLink(w, token, 201)
}

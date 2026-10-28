package portal

import (
	"errors"
	"net/http"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/httpapi"
)

// Handler serves the portal API. The admin-only routes below live here because
// this package owns `user_sessions`: the module rules of AGENTS section 24 forbid
// another module writing that table, and the user module must not learn about it.
type Handler struct {
	Repository *Repository
	Service    *Service
	Auth       *auth.Handler
	Audit      auth.Audit
}

// Register wires Owner account management and the separate end-user API.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/users/{id}/portal-preview", h.Auth.Require(http.HandlerFunc(h.preview), true))
	mux.Handle("PUT /api/v1/users/{id}/login", h.Auth.Require(http.HandlerFunc(h.setLogin), true))
	mux.Handle("DELETE /api/v1/users/{id}/sessions", h.Auth.Require(http.HandlerFunc(h.deleteSessions), true))
	mux.HandleFunc("POST /api/v1/portal/auth/login", h.login)
	mux.Handle("POST /api/v1/portal/auth/logout", h.RequirePortal(http.HandlerFunc(h.logout)))
	mux.Handle("GET /api/v1/portal/auth/session", h.RequirePortal(http.HandlerFunc(h.session)))
	mux.Handle("PUT /api/v1/portal/auth/password", h.RequirePortal(http.HandlerFunc(h.password)))
	mux.Handle("GET /api/v1/portal/devices-online", h.RequirePortal(http.HandlerFunc(h.devicesOnline)))
	mux.Handle("GET /api/v1/portal/me", h.RequirePortal(http.HandlerFunc(h.me)))
	mux.Handle("GET /api/v1/portal/traffic", h.RequirePortal(http.HandlerFunc(h.traffic)))
	mux.Handle("GET /api/v1/portal/nodes", h.RequirePortal(http.HandlerFunc(h.nodes)))
	mux.Handle("GET /api/v1/portal/subscription", h.RequirePortal(http.HandlerFunc(h.subscription)))
	mux.Handle("POST /api/v1/portal/subscription/reset", h.RequirePortal(http.HandlerFunc(h.resetSubscription)))
}

// setLogin turns the user's portal login on or off and optionally sets a new
// password. The password rules are the administrators' own, so a user account can
// never be weaker than an admin account.
func (h *Handler) setLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled  bool   `json:"enabled"`
		Password string `json:"password"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	userID := r.PathValue("id")
	account, err := h.Repository.Account(r.Context(), userID)
	if err != nil {
		h.fail(w, err)
		return
	}
	hash := ""
	if body.Password != "" {
		if hash, err = auth.HashPassword(body.Password); err != nil {
			httpapi.Error(w, 400, "invalid_request", err.Error())
			return
		}
	} else if body.Enabled && account.PasswordHash == "" {
		httpapi.Error(w, 400, "invalid_request", ErrNoPassword.Error())
		return
	}
	if err := h.Repository.SetLogin(r.Context(), userID, body.Enabled, hash); err != nil {
		h.fail(w, err)
		return
	}
	action := "启用用户登录"
	if !body.Enabled {
		action = "停用用户登录"
	} else if hash != "" {
		action = "设置用户登录密码"
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, action, account.Username, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

// deleteSessions ends every portal session of the user.
func (h *Handler) deleteSessions(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	account, err := h.Repository.Account(r.Context(), userID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Repository.DeleteSessions(r.Context(), userID); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "撤销用户登录会话", account.Username, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrCredentials), errors.Is(err, auth.ErrUnauthorized), errors.Is(err, ErrLoginDisabled), errors.Is(err, ErrLoginUnavailable):
		httpapi.Error(w, 401, "unauthorized", "Authentication required")
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpapi.Error(w, 429, "rate_limited", "Too many login attempts")
	case errors.Is(err, ErrNotFound), errors.Is(err, user.ErrNotFound):
		httpapi.Error(w, 404, "not_found", "User not found")
	default:
		h.Auth.Logger.Error("portal operation failed", "error", err)
		httpapi.Error(w, 500, "internal_error", "Portal operation failed")
	}
}

// preview uses the same read paths as the portal and never creates a session or token.
func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	me, err := h.Service.Me(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	nodes, err := h.Service.GrantedNodes(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	sub, err := h.subscriptionView(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"me": me, "nodes": map[string]any{"items": nodes}, "subscription": sub})
}

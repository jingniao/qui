package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"proxycontrol/internal/platform/httpapi"
	"strings"
	"time"
)

type Handler struct {
	Service        *Service
	Secure         bool
	Logger         *slog.Logger
	TrustedProxies []*net.IPNet
}

func (h *Handler) cookieName() string {
	if h.Secure {
		return "__Host-proxy_session"
	}
	return "proxy_session"
}
func (h *Handler) token(r *http.Request) string {
	cookie, err := r.Cookie(h.cookieName())
	if err != nil {
		return ""
	}
	return cookie.Value
}
func (h *Handler) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: h.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
}
func (h *Handler) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: h.cookieName(), Path: "/", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// requestIP resolves the client address. Forwarding headers are honoured only
// when the direct peer is a configured trusted proxy; otherwise the peer
// address is authoritative.
func (h *Handler) requestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !h.trustedProxy(peer) {
		return host
	}
	// The trusted proxy appends the address it saw to the right of anything the
	// client sent, so the leftmost entries are client-controlled. Walk from the
	// right and take the first valid IP that is not itself a trusted proxy.
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(parts[i])
			ip := net.ParseIP(candidate)
			if ip == nil || h.trustedProxy(ip) {
				continue
			}
			return candidate
		}
	}
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(real) != nil {
		return real
	}
	return host
}

func (h *Handler) trustedProxy(peer net.IP) bool {
	for _, network := range h.TrustedProxies {
		if network.Contains(peer) {
			return true
		}
	}
	return false
}

func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return parsed.Scheme == scheme && strings.EqualFold(parsed.Host, r.Host) && parsed.User == nil
}

func (h *Handler) Require(next http.Handler, owner bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := h.Service.Session(r.Context(), h.token(r))
		if err != nil {
			h.fail(w, err)
			return
		}
		if !session.IsAuthenticated {
			httpapi.Error(w, 401, "unauthorized", "Authentication required")
			return
		}
		if owner && session.CurrentUser.Role != "Owner" {
			httpapi.Error(w, 403, "forbidden", "Owner permission required")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRFToken)) != 1 {
				httpapi.Error(w, 403, "csrf_failed", "CSRF validation failed")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, session.CurrentUser)))
	})
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/session", h.session)
	mux.HandleFunc("POST /api/v1/auth/setup", h.setup)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.Handle("POST /api/v1/auth/logout", h.Require(http.HandlerFunc(h.logout), false))
	mux.Handle("PUT /api/v1/auth/password", h.Require(http.HandlerFunc(h.password), false))
	mux.Handle("GET /api/v1/admins", h.Require(http.HandlerFunc(h.listAdmins), true))
	mux.Handle("POST /api/v1/admins", h.Require(http.HandlerFunc(h.createAdmin), true))
	mux.Handle("PUT /api/v1/admins/{id}", h.Require(http.HandlerFunc(h.updateAdmin), true))
	mux.Handle("DELETE /api/v1/admins/{id}", h.Require(http.HandlerFunc(h.deleteAdmin), true))
	mux.Handle("POST /api/v1/admins/{id}/password", h.Require(http.HandlerFunc(h.resetPassword), true))
	mux.Handle("POST /api/v1/admins/{id}/revoke-sessions", h.Require(http.HandlerFunc(h.revokeSessions), true))
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) {
	session, err := h.Service.Session(r.Context(), h.token(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, session)
}
func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		httpapi.Error(w, 403, "csrf_failed", "Cross-origin setup denied")
		return
	}
	var body struct {
		Token        string `json:"token"`
		Username     string `json:"username"`
		Password     string `json:"password"`
		PlatformName string `json:"platformName"`
		Timezone     string `json:"timezone"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	token, session, err := h.Service.Setup(r.Context(), body.Token, body.Username, body.Password, body.PlatformName, body.Timezone, h.requestIP(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.setCookie(w, token, session.ExpiresAt)
	httpapi.JSON(w, 201, session)
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
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
	token, session, err := h.Service.Login(r.Context(), body.Username, body.Password, h.requestIP(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.setCookie(w, token, session.ExpiresAt)
	httpapi.JSON(w, 200, session)
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Repository.DeleteSession(r.Context(), tokenHash(h.token(r))); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Service.Audit.Record(r.Context(), Actor(r.Context()).Username, "退出登录", "后台", "成功", h.requestIP(r)); err != nil {
		h.fail(w, err)
		return
	}
	h.clearCookie(w)
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
	actor := Actor(r.Context())
	if err := h.Service.ChangePassword(r.Context(), actor, actor.ID, body.CurrentPassword, body.NewPassword, h.requestIP(r), false); err != nil {
		h.fail(w, err)
		return
	}
	h.clearCookie(w)
	w.WriteHeader(204)
}
func (h *Handler) listAdmins(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	admins, total, err := h.Service.Repository.List(r.Context(), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": admins, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) createAdmin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
		Role        string `json:"role"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	admin, err := h.Service.CreateAdmin(r.Context(), Actor(r.Context()), body.Username, body.DisplayName, body.Password, body.Role, h.requestIP(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 201, admin)
}
func (h *Handler) updateAdmin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName string `json:"displayName"`
		Status      string `json:"status"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if err := h.Service.UpdateAdmin(r.Context(), Actor(r.Context()), r.PathValue("id"), body.DisplayName, body.Status, h.requestIP(r)); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) deleteAdmin(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.DeleteAdmin(r.Context(), Actor(r.Context()), r.PathValue("id"), h.requestIP(r)); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if err := h.Service.ChangePassword(r.Context(), Actor(r.Context()), r.PathValue("id"), "", body.Password, h.requestIP(r), true); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) revokeSessions(w http.ResponseWriter, r *http.Request) {
	admin, err := h.Service.Repository.FindID(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if admin.Role == "Owner" {
		h.fail(w, ErrOwnerProtected)
		return
	}
	if err := h.Service.Repository.RevokeSessions(r.Context(), admin.ID); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Service.Audit.Record(r.Context(), Actor(r.Context()).Username, "撤销管理员会话", admin.Username, "成功", h.requestIP(r)); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrCredentials), errors.Is(err, ErrUnauthorized):
		httpapi.Error(w, 401, "invalid_credentials", err.Error())
	case errors.Is(err, ErrInitialized), errors.Is(err, ErrConflict):
		httpapi.Error(w, 409, "conflict", err.Error())
	case errors.Is(err, ErrSetupToken):
		httpapi.Error(w, 403, "setup_token_required", err.Error())
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpapi.Error(w, 429, "rate_limited", err.Error())
	case errors.Is(err, ErrOwnerProtected):
		httpapi.Error(w, 403, "owner_protected", err.Error())
	case errors.Is(err, ErrNotFound):
		httpapi.Error(w, 404, "not_found", err.Error())
	default:
		message := err.Error()
		if strings.HasPrefix(message, "password must") || strings.HasPrefix(message, "username must") || strings.HasPrefix(message, "invalid ") || strings.HasPrefix(message, "new administrators") || strings.HasPrefix(message, "display name") || strings.HasPrefix(message, "platform name") {
			httpapi.Error(w, 400, "invalid_request", message)
			return
		}
		h.Logger.Error("authentication operation failed", "error", err)
		httpapi.Error(w, 500, "internal_error", "Operation failed")
	}
}

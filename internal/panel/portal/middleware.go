package portal

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

type sessionKey struct{}

func (h *Handler) cookieName() string {
	if h.Auth.Secure {
		return "__Host-qui_portal_session"
	}
	return "qui_portal_session"
}

func (h *Handler) token(r *http.Request) string {
	cookie, err := r.Cookie(h.cookieName())
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (h *Handler) setCookie(w http.ResponseWriter, token string, expiry time.Time) {
	age := int(time.Until(expiry).Seconds())
	if token == "" {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{Name: h.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: h.Auth.Secure, SameSite: http.SameSiteStrictMode, Expires: expiry, MaxAge: age})
}

// RequirePortal resolves only the portal cookie and table, never admin roles.
func (h *Handler) RequirePortal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		session, err := h.Service.Session(r.Context(), h.token(r))
		if err != nil {
			h.fail(w, err)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if !auth.SameOrigin(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRFToken)) != 1 {
				httpapi.Error(w, 403, "csrf_failed", "CSRF validation failed")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, session)))
	})
}

func current(r *http.Request) Session { return r.Context().Value(sessionKey{}).(Session) }

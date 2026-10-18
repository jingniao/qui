package auth

import (
	"net/http"
	"time"
)

// Test-only exports so the external auth_test package can exercise unexported
// helpers without widening the production API.
var (
	VerifyPassword = verifyPassword
	TokenHash      = tokenHash
	RequestIP      = (*Handler).requestIP
)

func SetCookie(handler *Handler, w http.ResponseWriter, token string, expires time.Time) {
	handler.setCookie(w, token, expires)
}

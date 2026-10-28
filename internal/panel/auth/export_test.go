package auth

import (
	"net/http"
	"time"
)

// Test-only exports so the external auth_test package can exercise unexported
// helpers without widening the production API. VerifyPassword and the token hash
// are exported for the portal (portal.go), so only these remain test-only.
var RequestIP = (*Handler).requestIP

func SetCookie(handler *Handler, w http.ResponseWriter, token string, expires time.Time) {
	handler.setCookie(w, token, expires)
}

package auth

import (
	"encoding/base64"
	"fmt"
	"net/http"
)

// DummyPasswordHash keeps unknown accounts on the normal password verification path.
func DummyPasswordHash() string {
	return fmt.Sprintf("pbkdf2-sha256$600000$%s$%s", base64.RawStdEncoding.EncodeToString(make([]byte, 16)), base64.RawStdEncoding.EncodeToString(make([]byte, 32)))
}

// The end-user portal (plan section 4.2) shares the admin password rules, token
// hashing and CSRF scheme instead of copying them: two copies of a password
// verifier or of a same-origin check drift apart, and a weaker copy in the
// portal would silently lower the bar for half the accounts. These wrappers are
// the only way the portal reaches them.

// HashPassword validates a password with the admin rules and hashes it.
func HashPassword(password string) (string, error) { return hashPassword(password) }

// VerifyPassword reports whether the password matches the stored hash.
func VerifyPassword(encoded, password string) bool { return verifyPassword(encoded, password) }

// ValidatePassword applies the admin password rules.
func ValidatePassword(password string) error { return validatePassword(password) }

// NewToken returns a fresh random session token (43 URL-safe characters).
func NewToken() (string, error) { return randomToken() }

// HashToken is the SHA-256 lookup key of a session token.
func HashToken(token string) string { return tokenHash(token) }

// CSRF derives the CSRF token a session must send in X-CSRF-Token.
func CSRF(token string) string { return csrfToken(token) }

// SameOrigin reports whether the request passes the same-origin check.
func SameOrigin(r *http.Request) bool { return sameOrigin(r) }

// ClientIP applies the same trusted-proxy policy to portal and admin logins.
func (h *Handler) ClientIP(r *http.Request) string { return h.requestIP(r) }

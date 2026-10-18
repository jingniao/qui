package auth

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInitialized    = errors.New("system already initialized")
	ErrSetupToken     = errors.New("invalid or missing first-run setup token")
	ErrCredentials    = errors.New("invalid username or password")
	ErrRateLimited    = errors.New("too many login attempts; try again after 15 minutes")
	ErrUnauthorized   = errors.New("authentication required")
	ErrOwnerProtected = errors.New("Owner account is protected")
	ErrConflict       = errors.New("username already exists")
	ErrNotFound       = errors.New("administrator not found")
	ErrNoOwner        = errors.New("no Owner yet; finish the web setup first")
)

type Admin struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
	Status       string `json:"status"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"createdAt"`
	LastLoginAt  string `json:"lastLoginAt"`
	LoginIP      string `json:"loginIp"`
}

type Session struct {
	IsAuthenticated bool      `json:"isAuthenticated"`
	IsInitialized   bool      `json:"isInitialized"`
	CurrentUser     *Admin    `json:"currentUser"`
	CSRFToken       string    `json:"csrfToken"`
	ExpiresAt       time.Time `json:"expiresAt,omitempty"`
	PlatformName    string    `json:"platformName"`
	Timezone        string    `json:"timezone"`
}

type actorKey struct{}

func Actor(ctx context.Context) *Admin { user, _ := ctx.Value(actorKey{}).(*Admin); return user }

type Audit interface {
	Record(context.Context, string, string, string, string, string) error
}

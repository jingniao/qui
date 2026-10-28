package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

type SetupSettings interface {
	SaveSetup(context.Context, *sql.Tx, string, string) error
	ReadSetup(context.Context) (string, string, error)
}
type Service struct {
	Repository      *Repository
	Audit           Audit
	Settings        SetupSettings
	SessionDuration time.Duration

	// setupMu guards the one-time first-run setup token. The token is set from
	// outside when no Owner exists and cleared once an Owner is created.
	setupMu        sync.Mutex
	setupToken     string
	setupTokenPath string

	// stateMu guards the cached first-run and base-setting state. It changes
	// only when an Owner is created or the platform settings are saved, so
	// caching it keeps an authenticated request at one session lookup.
	stateMu    sync.Mutex
	cached     setupState
	stateValid bool

	limiter loginLimiter
}

// setupState is the part of every session answer that does not depend on the
// caller's token.
type setupState struct {
	initialized  bool
	platformName string
	timezone     string
}

// RefreshSetupState reloads the cached first-run and base-setting state. The
// panel calls it when it starts and whenever the platform settings change, so
// requests do not pay for the lookup.
func (s *Service) RefreshSetupState(ctx context.Context) error {
	initialized, err := s.Repository.Initialized(ctx)
	if err != nil {
		return err
	}
	name, timezone, err := s.Settings.ReadSetup(ctx)
	if err != nil {
		return err
	}
	s.stateMu.Lock()
	s.cached = setupState{initialized: initialized, platformName: name, timezone: timezone}
	s.stateValid = true
	s.stateMu.Unlock()
	return nil
}

// InvalidateSetupState drops the cached state so the next request reloads it.
func (s *Service) InvalidateSetupState() {
	s.stateMu.Lock()
	s.stateValid = false
	s.stateMu.Unlock()
}

// setupState returns the cached state, loading it once if the cache is cold.
func (s *Service) setupState(ctx context.Context) (setupState, error) {
	s.stateMu.Lock()
	cached, valid := s.cached, s.stateValid
	s.stateMu.Unlock()
	if valid {
		return cached, nil
	}
	if err := s.RefreshSetupState(ctx); err != nil {
		return setupState{}, err
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.cached, nil
}

// SetSetupToken publishes the one-time first-run setup token and the path of
// the file that holds it.
func (s *Service) SetSetupToken(token, path string) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	s.setupToken = token
	s.setupTokenPath = path
}

// CurrentSetupToken returns the one-time first-run setup token, or "" once an
// Owner exists or setup has completed.
func (s *Service) CurrentSetupToken() string {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	return s.setupToken
}

// CurrentSetupTokenPath returns the path of the on-disk setup token file, or ""
// when there is none.
func (s *Service) CurrentSetupTokenPath() string {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	return s.setupTokenPath
}

const (
	loginAttemptLimit  = 5
	loginAttemptWindow = 15 * time.Minute
	loginAttemptKeys   = 4096
)

// loginLimiter throttles login attempts in memory, keyed by both the client IP
// and the username, so rotating either one cannot bypass the limit. Restarts
// reset it, which is acceptable for the small user base this panel serves.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func loginKey(kind, value string) string { return kind + ":" + value }

func (l *loginLimiter) reserve(keys ...string) error {
	now := time.Now()
	cutoff := now.Add(-loginAttemptWindow)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.attempts == nil {
		l.attempts = make(map[string][]time.Time)
	}
	if len(l.attempts) > loginAttemptKeys {
		for key := range l.attempts {
			l.pruneLocked(key, cutoff)
		}
	}
	for _, key := range keys {
		l.pruneLocked(key, cutoff)
	}
	for _, key := range keys {
		if len(l.attempts[key]) >= loginAttemptLimit {
			return ErrRateLimited
		}
	}
	for _, key := range keys {
		l.attempts[key] = append(l.attempts[key], now)
	}
	return nil
}

func (l *loginLimiter) clear(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		delete(l.attempts, key)
	}
}

func (l *loginLimiter) pruneLocked(key string, cutoff time.Time) {
	kept := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(l.attempts, key)
		return
	}
	l.attempts[key] = kept
}

// ReserveLogin records one login attempt against the client IP and the
// username. Either bucket reaching the limit blocks the attempt.
func (s *Service) ReserveLogin(_ context.Context, ip, username string) error {
	return s.limiter.reserve(loginKey("ip", ip), loginKey("user", strings.TrimSpace(username)))
}

// ClearLoginAttempts resets both buckets after a successful authentication.
func (s *Service) ClearLoginAttempts(_ context.Context, ip, username string) error {
	s.limiter.clear(loginKey("ip", ip), loginKey("user", strings.TrimSpace(username)))
	return nil
}

func (s *Service) Setup(ctx context.Context, token, username, password, platformName, timezone, ip string) (string, Session, error) {
	initialized, err := s.Repository.Initialized(ctx)
	if err != nil {
		return "", Session{}, err
	}
	if initialized {
		return "", Session{}, ErrInitialized
	}
	// Reject a wrong token before the expensive password hash; the check is
	// repeated under the lock in initializeWithSetupToken.
	s.setupMu.Lock()
	valid := s.validSetupTokenLocked(token)
	s.setupMu.Unlock()
	if !valid {
		return "", Session{}, ErrSetupToken
	}
	if err := validateUsername(username); err != nil {
		return "", Session{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return "", Session{}, err
	}
	id, err := randomToken()
	if err != nil {
		return "", Session{}, err
	}
	admin := Admin{ID: id, Username: username, DisplayName: username, Role: "Owner", Status: "active", PasswordHash: hash, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := s.initializeWithSetupToken(ctx, token, admin, func(ctx context.Context, tx *sql.Tx) error {
		return s.Settings.SaveSetup(ctx, tx, platformName, timezone)
	}); err != nil {
		return "", Session{}, err
	}
	// The Owner now exists and the platform name and timezone are stored; cache
	// them so the first authenticated requests stay at one query.
	if err := s.RefreshSetupState(ctx); err != nil {
		s.InvalidateSetupState()
	}
	if err := s.Audit.Record(ctx, username, "初始化系统", username, "成功", ip); err != nil {
		return "", Session{}, err
	}
	return s.newSession(ctx, &admin, ip)
}

// initializeWithSetupToken holds the token lock across the token check, the
// Owner creation and the token consumption, so two concurrent setups with the
// correct token cannot both pass the check.
func (s *Service) initializeWithSetupToken(ctx context.Context, token string, admin Admin, settings func(context.Context, *sql.Tx) error) error {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if !s.validSetupTokenLocked(token) {
		return ErrSetupToken
	}
	if err := s.Repository.Initialize(ctx, admin, settings); err != nil {
		return err
	}
	s.consumeSetupTokenLocked()
	return nil
}

// validSetupTokenLocked compares the presented first-run token in constant
// time. The caller must hold setupMu.
func (s *Service) validSetupTokenLocked(token string) bool {
	if s.setupToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.setupToken)) == 1
}

// consumeSetupTokenLocked invalidates the token after a successful setup and
// removes the on-disk copy so it cannot be reused. The caller must hold setupMu.
func (s *Service) consumeSetupTokenLocked() {
	s.setupToken = ""
	if s.setupTokenPath != "" {
		os.Remove(s.setupTokenPath)
		s.setupTokenPath = ""
	}
}

func (s *Service) Login(ctx context.Context, username, password, ip string) (string, Session, error) {
	if err := s.ReserveLogin(ctx, ip, username); err != nil {
		return "", Session{}, err
	}
	admin, err := s.Repository.FindUsername(ctx, username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", Session{}, err
	}
	dummy := DummyPasswordHash()
	hash := dummy
	if err == nil {
		hash = admin.PasswordHash
	}
	if len(password) > 512 || !verifyPassword(hash, password) || err != nil || admin.Status != "active" {
		if auditErr := s.Audit.Record(ctx, strings.TrimSpace(username), "登录", "后台", "失败", ip); auditErr != nil {
			return "", Session{}, auditErr
		}
		return "", Session{}, ErrCredentials
	}
	if err := s.ClearLoginAttempts(ctx, ip, username); err != nil {
		return "", Session{}, err
	}
	token, session, err := s.newSession(ctx, admin, ip)
	if err != nil {
		return "", Session{}, err
	}
	if err := s.Audit.Record(ctx, username, "登录", "后台", "成功", ip); err != nil {
		s.Repository.DeleteSession(ctx, tokenHash(token))
		return "", Session{}, err
	}
	return token, session, nil
}

func (s *Service) newSession(ctx context.Context, admin *Admin, ip string) (string, Session, error) {
	token, err := randomToken()
	if err != nil {
		return "", Session{}, err
	}
	duration := s.SessionDuration
	if settings, ok := s.Settings.(interface {
		SessionLifetime(context.Context) (time.Duration, error)
	}); ok {
		configured, err := settings.SessionLifetime(ctx)
		if err != nil {
			return "", Session{}, err
		}
		duration = configured
	}
	expires := time.Now().UTC().Add(duration)
	if err := s.Repository.CreateSession(ctx, tokenHash(token), admin.ID, ip, expires); err != nil {
		return "", Session{}, err
	}
	view, err := s.Session(ctx, token)
	if err != nil {
		s.Repository.DeleteSession(ctx, tokenHash(token))
		return "", Session{}, err
	}
	return token, view, nil
}

func (s *Service) Session(ctx context.Context, token string) (Session, error) {
	state, err := s.setupState(ctx)
	if err != nil {
		return Session{}, err
	}
	session := Session{IsInitialized: state.initialized, PlatformName: state.platformName, Timezone: state.timezone}
	if token == "" || len(token) > 128 {
		return session, nil
	}
	admin, expires, err := s.Repository.Session(ctx, tokenHash(token))
	if errors.Is(err, ErrUnauthorized) {
		return session, nil
	}
	if err != nil {
		return session, err
	}
	session.IsAuthenticated = true
	session.CurrentUser = admin
	session.ExpiresAt = expires
	session.CSRFToken = csrfToken(token)
	return session, nil
}

func (s *Service) CreateAdmin(ctx context.Context, actor *Admin, username, displayName, password, role, ip string) (Admin, error) {
	if actor == nil || actor.Role != "Owner" {
		return Admin{}, ErrUnauthorized
	}
	if role != "Viewer" {
		return Admin{}, errors.New("new administrators must have Viewer role")
	}
	if err := validateUsername(username); err != nil {
		return Admin{}, err
	}
	if len(displayName) > 120 {
		return Admin{}, errors.New("display name too long")
	}
	if _, err := s.Repository.FindUsername(ctx, username); err == nil {
		return Admin{}, ErrConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Admin{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Admin{}, err
	}
	id, err := randomToken()
	if err != nil {
		return Admin{}, err
	}
	admin := Admin{ID: id, Username: username, DisplayName: displayName, Role: role, Status: "active", PasswordHash: hash, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := s.Repository.CreateAdmin(ctx, admin); err != nil {
		return Admin{}, err
	}
	if err := s.Audit.Record(ctx, actor.Username, "创建管理员", username, "成功", ip); err != nil {
		return Admin{}, err
	}
	return admin, nil
}

func (s *Service) UpdateAdmin(ctx context.Context, actor *Admin, id, displayName, status, ip string) error {
	if actor == nil || actor.Role != "Owner" {
		return ErrUnauthorized
	}
	if actor == nil || actor.Role != "Owner" {
		return ErrUnauthorized
	}
	admin, err := s.Repository.FindID(ctx, id)
	if err != nil {
		return err
	}
	if admin.Role == "Owner" {
		return ErrOwnerProtected
	}
	if status != "active" && status != "disabled" {
		return errors.New("invalid administrator status")
	}
	if len(displayName) > 120 {
		return errors.New("display name too long")
	}
	admin.Status, admin.DisplayName = status, displayName
	if err := s.Repository.UpdateAdmin(ctx, *admin); err != nil {
		return err
	}
	return s.Audit.Record(ctx, actor.Username, "修改管理员", admin.Username, "成功", ip)
}

func (s *Service) DeleteAdmin(ctx context.Context, actor *Admin, id, ip string) error {
	if actor == nil || actor.Role != "Owner" {
		return ErrUnauthorized
	}
	if actor == nil || actor.Role != "Owner" {
		return ErrUnauthorized
	}
	admin, err := s.Repository.FindID(ctx, id)
	if err != nil {
		return err
	}
	if admin.Role == "Owner" {
		return ErrOwnerProtected
	}
	if err := s.Repository.DeleteAdmin(ctx, id); err != nil {
		return err
	}
	return s.Audit.Record(ctx, actor.Username, "删除管理员", admin.Username, "成功", ip)
}

func (s *Service) ChangePassword(ctx context.Context, actor *Admin, id, current, newPassword, ip string, reset bool) error {
	if actor == nil {
		return ErrUnauthorized
	}
	if actor == nil {
		return ErrUnauthorized
	}
	admin, err := s.Repository.FindID(ctx, id)
	if err != nil {
		return err
	}
	if reset {
		if actor.Role != "Owner" || admin.Role == "Owner" {
			return ErrOwnerProtected
		}
	} else if actor.ID != id || !verifyPassword(admin.PasswordHash, current) {
		return ErrCredentials
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.Repository.ChangePassword(ctx, id, hash); err != nil {
		return err
	}
	return s.Audit.Record(ctx, actor.Username, "修改管理员密码", admin.Username, "成功", ip)
}

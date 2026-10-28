// Package portal serves the end-user side of v1.1: the account columns of
// migration 0114 and the `user_sessions` table. End-user sessions are a third
// principal kind next to the admin accounts (plan section 4.2): a different
// cookie, a different table and a different middleware, so the two session kinds
// reject each other by construction rather than by a flag.
package portal

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"proxycontrol/internal/panel/user"
)

var (
	// ErrNotFound reports a proxy user that does not exist.
	ErrNotFound = errors.New("user not found")
	// ErrLoginDisabled reports a user the Owner has not given a login.
	ErrLoginDisabled = errors.New("login is not enabled for this user")
	// ErrLoginUnavailable reports a login the Owner disabled again.
	ErrLoginUnavailable = errors.New("login is disabled")
	// ErrNoPassword reports an enable request without a password for a user that
	// has none yet.
	ErrNoPassword = errors.New("a password is required to enable the login")
)

// Repository owns the account columns and the end-user sessions. The user record
// itself is read through the user module wherever its other fields are needed.
type Repository struct {
	Database *sql.DB
}

func timestamp(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000000000Z") }

// Account is the login-relevant part of a user.
type Account struct {
	ID           string
	Username     string
	DisplayName  string
	Status       string
	PasswordHash string
	LoginEnabled bool
	LastLoginAt  string
}

// Account reads the login columns of one user.
func (r *Repository) Account(ctx context.Context, userID string) (Account, error) {
	var account Account
	u, err := (&user.Repository{Database: r.Database}).Get(ctx, userID)
	if errors.Is(err, user.ErrNotFound) {
		return account, ErrNotFound
	}
	if err != nil {
		return account, err
	}
	account.ID, account.Username, account.DisplayName, account.Status = u.ID, u.Username, u.DisplayName, u.Status
	var enabled int
	err = r.Database.QueryRowContext(ctx, "SELECT password_hash,login_enabled,last_login_at FROM users WHERE id=?", userID).
		Scan(&account.PasswordHash, &enabled, &account.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return account, ErrNotFound
	}
	account.LoginEnabled = enabled == 1
	return account, err
}

// SetLogin enables or disables the login and, when a password is supplied,
// replaces it. Any change of the password and disabling both end the user's
// sessions, so a reset takes effect on the next request (A6.4).
func (r *Repository) SetLogin(ctx context.Context, userID string, enabled bool, passwordHash string) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if passwordHash != "" {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash=?,login_enabled=? WHERE id=?", passwordHash, boolInt(enabled), userID); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, "UPDATE users SET login_enabled=? WHERE id=?", boolInt(enabled), userID); err != nil {
		return err
	}
	if passwordHash != "" || !enabled {
		if _, err := tx.ExecContext(ctx, "DELETE FROM user_sessions WHERE user_id=?", userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteSessions ends every session of one user.
func (r *Repository) DeleteSessions(ctx context.Context, userID string) error {
	_, err := r.Database.ExecContext(ctx, "DELETE FROM user_sessions WHERE user_id=?", userID)
	return err
}

// CreateSession stores the hash of a session token. Only the hash is kept, as for
// admin sessions.
func (r *Repository) CreateSession(ctx context.Context, tokenHash, userID string, expires time.Time) error {
	return r.createSession(ctx, tokenHash, userID, "", expires)
}

func (r *Repository) createSession(ctx context.Context, tokenHash, userID, passwordHash string, expires time.Time) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Like admin sessions, expired portal rows are purged on session creation.
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_sessions WHERE expires_at<=?", timestamp(time.Now())); err != nil {
		return err
	}
	// Recheck the login switch inside the write transaction so disabling login
	// cannot race a password verification into creating a usable session.
	result, err := tx.ExecContext(ctx, "INSERT INTO user_sessions(token_hash,user_id,expires_at,created_at) SELECT ?,id,?,? FROM users WHERE id=? AND login_enabled=1 AND (?='' OR password_hash=?)", tokenHash, timestamp(expires), timestamp(time.Now()), userID, passwordHash, passwordHash)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count != 1 {
		return ErrLoginDisabled
	}
	return tx.Commit()
}

// AccountByUsername resolves only portal login material; profile reads use user.Repository.
func (r *Repository) AccountByUsername(ctx context.Context, username string) (Account, error) {
	u, err := (&user.Repository{Database: r.Database}).ByUsername(ctx, username)
	if errors.Is(err, user.ErrNotFound) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return r.Account(ctx, u.ID)
}

// SessionExpiry is kept alongside the session lookup rather than exposing stored tokens.
func (r *Repository) SessionExpiry(ctx context.Context, hash string) (time.Time, error) {
	var value string
	if err := r.Database.QueryRowContext(ctx, "SELECT expires_at FROM user_sessions WHERE token_hash=?", hash).Scan(&value); err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, value)
}

// ChangePassword atomically changes the password and revokes every other session.
// The expected hash prevents a concurrent Owner reset from being overwritten.
func (r *Repository) ChangePassword(ctx context.Context, userID, expected, replacement, keep string) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE users SET password_hash=? WHERE id=? AND password_hash=? AND login_enabled=1 AND EXISTS(SELECT 1 FROM user_sessions WHERE user_id=? AND token_hash=?)", replacement, userID, expected, userID, keep)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrLoginUnavailable
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_sessions WHERE user_id=? AND token_hash<>?", userID, keep); err != nil {
		return err
	}
	return tx.Commit()
}

// SessionUser returns the user a live session belongs to. An expired row is
// removed on the way out, so a stale cookie cannot be revived by a clock skip.
func (r *Repository) SessionUser(ctx context.Context, tokenHash string) (string, error) {
	var userID, expires string
	err := r.Database.QueryRowContext(ctx, "SELECT user_id,expires_at FROM user_sessions WHERE token_hash=?", tokenHash).Scan(&userID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", err
	}
	deadline, err := time.Parse("2006-01-02T15:04:05.000000000Z", expires)
	if err != nil || time.Now().After(deadline) {
		_, _ = r.Database.ExecContext(ctx, "DELETE FROM user_sessions WHERE token_hash=?", tokenHash)
		return "", sql.ErrNoRows
	}
	return userID, nil
}

// DeleteSession ends one session (logout).
func (r *Repository) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.Database.ExecContext(ctx, "DELETE FROM user_sessions WHERE token_hash=?", tokenHash)
	return err
}

// DeleteOtherSessions ends every session of the user except the one given.
func (r *Repository) DeleteOtherSessions(ctx context.Context, userID, keepTokenHash string) error {
	_, err := r.Database.ExecContext(ctx, "DELETE FROM user_sessions WHERE user_id=? AND token_hash<>?", userID, keepTokenHash)
	return err
}

// TouchLogin records a successful login.
func (r *Repository) TouchLogin(ctx context.Context, userID string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE users SET last_login_at=? WHERE id=?", timestamp(time.Now()), userID)
	return err
}

// SetPassword replaces the stored hash without touching the login switch; the
// change of password ends the other sessions in the caller.
func (r *Repository) SetPassword(ctx context.Context, userID, passwordHash string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE users SET password_hash=? WHERE id=?", passwordHash, userID)
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

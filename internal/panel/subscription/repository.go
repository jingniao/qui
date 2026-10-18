package subscription

import (
	"context"
	"database/sql"
	"errors"
	"proxycontrol/internal/platform/id"
	"time"
)

var ErrToken = errors.New("subscription token unavailable")

// Repository owns subscription tokens. Reader is the optional read-only pool,
// used for the token lookup and the list; writes always use Database.
type Repository struct {
	Database *sql.DB
	Reader   *sql.DB
}

func (r *Repository) reader() *sql.DB {
	if r.Reader != nil {
		return r.Reader
	}
	return r.Database
}

type Token struct {
	ID            string `json:"id"`
	UserID        string `json:"userId"`
	DeviceID      string `json:"deviceId"`
	CreatedAt     string `json:"createdAt"`
	LastFetchedAt string `json:"lastFetchedAt"`
	RevokedAt     string `json:"revokedAt"`
}

func timestamp(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func (r *Repository) Rotate(ctx context.Context, userID, deviceID string) (Token, string, error) {
	tokenID, err := id.New()
	if err != nil {
		return Token{}, "", err
	}
	plaintext, err := id.New()
	if err != nil {
		return Token{}, "", err
	}
	token := Token{ID: tokenID, UserID: userID, DeviceID: deviceID, CreatedAt: timestamp(time.Now())}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return token, "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE subscription_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", token.CreatedAt, userID); err != nil {
		return token, "", err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO subscription_tokens(id,user_id,device_id,token_hash,created_at) VALUES(?,?,?,?,?)", token.ID, userID, deviceID, id.Hash(plaintext), token.CreatedAt); err != nil {
		return token, "", err
	}
	if err := tx.Commit(); err != nil {
		return token, "", err
	}
	return token, plaintext, nil
}
func (r *Repository) Resolve(ctx context.Context, plaintext string) (Token, error) {
	var token Token
	if len(plaintext) != 43 {
		return token, ErrToken
	}
	err := r.reader().QueryRowContext(ctx, "SELECT id,user_id,device_id,created_at,last_fetched_at FROM subscription_tokens WHERE token_hash=? AND revoked_at IS NULL", id.Hash(plaintext)).Scan(&token.ID, &token.UserID, &token.DeviceID, &token.CreatedAt, &token.LastFetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrToken
	}
	return token, err
}
func (r *Repository) Fetched(ctx context.Context, tokenID string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE subscription_tokens SET last_fetched_at=? WHERE id=?", timestamp(time.Now()), tokenID)
	return err
}
func (r *Repository) Revoke(ctx context.Context, userID string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE subscription_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", timestamp(time.Now()), userID)
	return err
}
func (r *Repository) List(ctx context.Context, page, size int) ([]Token, int, error) {
	var count int
	if err := r.reader().QueryRowContext(ctx, "SELECT count(*) FROM subscription_tokens WHERE revoked_at IS NULL").Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.reader().QueryContext(ctx, "SELECT id,user_id,device_id,created_at,last_fetched_at FROM subscription_tokens WHERE revoked_at IS NULL ORDER BY created_at DESC,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	tokens := []Token{}
	for rows.Next() {
		var token Token
		if err := rows.Scan(&token.ID, &token.UserID, &token.DeviceID, &token.CreatedAt, &token.LastFetchedAt); err != nil {
			return nil, 0, err
		}
		tokens = append(tokens, token)
	}
	return tokens, count, rows.Err()
}

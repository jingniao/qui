package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"

	"proxycontrol/internal/platform/vault"
)

// validatePortal requires the 0114 material only when the archive applied that
// migration. Older archives are migrated by restore and remain valid inputs.
func validatePortal(ctx context.Context, database *sql.DB, configurationVault *vault.Vault) error {
	var applied, tables int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0114_end_user_accounts.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='user_sessions'").Scan(&tables); err != nil || applied != tables {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	// Reading the columns also detects a missing column in an empty table.
	rows, err := database.QueryContext(ctx, "SELECT password_hash,login_enabled,last_login_at FROM users")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var hash, last string
		var enabled int
		if err := rows.Scan(&hash, &enabled, &last); err != nil || enabled != 0 && enabled != 1 {
			rows.Close()
			return ErrArchive
		}
		if last != "" {
			if _, err := time.Parse(time.RFC3339Nano, last); err != nil {
				rows.Close()
				return ErrArchive
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	rows, err = database.QueryContext(ctx, "SELECT token_hash,user_id,expires_at,created_at FROM user_sessions")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var hash, userID, expires, created string
		if err := rows.Scan(&hash, &userID, &expires, &created); err != nil {
			rows.Close()
			return ErrArchive
		}
		// Session material is discarded on restore, just like admin sessions.
		// Verify its columns and timestamps without treating stored sessions as
		// reusable authentication material or imposing new hash-format rules.
		for _, value := range []string{expires, created} {
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				rows.Close()
				return ErrArchive
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	rows, err = database.QueryContext(ctx, "SELECT token_hash,CASE WHEN length(encrypted_token)<=4096 THEN encrypted_token END,encrypted_token IS NOT NULL FROM subscription_tokens")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		var encrypted []byte
		var present bool
		if err := rows.Scan(&hash, &encrypted, &present); err != nil {
			return ErrArchive
		}
		if !present {
			continue
		}
		plaintext, err := configurationVault.Open(encrypted)
		digest := sha256.Sum256(plaintext)
		clear(plaintext)
		if err != nil || hex.EncodeToString(digest[:]) != hash {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}

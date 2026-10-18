package subscription

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/db"
)

func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx, stamp string) error {
	if err := db.AssertRecoveryNone(ctx, tx, "SELECT count(*) FROM subscription_tokens s JOIN restore_source.subscription_tokens c ON c.id=s.id WHERE c.user_id<>s.user_id OR c.device_id<>s.device_id OR c.token_hash<>s.token_hash"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE subscription_tokens SET revoked_at=coalesce(revoked_at,?) WHERE NOT EXISTS(SELECT 1 FROM restore_source.subscription_tokens c WHERE c.id=subscription_tokens.id)", stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE subscription_tokens SET revoked_at=coalesce(revoked_at,(SELECT c.revoked_at FROM restore_source.subscription_tokens c WHERE c.id=subscription_tokens.id)) WHERE EXISTS(SELECT 1 FROM restore_source.subscription_tokens c WHERE c.id=subscription_tokens.id)"); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO subscription_tokens(id,user_id,device_id,token_hash,created_at,revoked_at,last_fetched_at)
SELECT id,user_id,device_id,token_hash,created_at,revoked_at,last_fetched_at FROM restore_source.subscription_tokens WHERE 1
ON CONFLICT(id) DO UPDATE SET revoked_at=coalesce(subscription_tokens.revoked_at,excluded.revoked_at),last_fetched_at=max(subscription_tokens.last_fetched_at,excluded.last_fetched_at)`)
	return err
}

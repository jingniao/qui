package access

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/db"
)

// Both authenticated snapshots may contain unfinished input. Preserve their
// union; a job absent from the newer snapshot can safely re-check the existing
// module cursors, but absence alone is never treated as a completion receipt.
func (r *Repository) PreservePendingTrafficRestoreTx(ctx context.Context, tx *sql.Tx) error {
	if err := db.AssertRecoveryNone(ctx, tx, "SELECT count(*) FROM traffic_pending_batches s JOIN restore_source.traffic_pending_batches c ON c.node_id=s.node_id AND c.batch_id=s.batch_id WHERE c.digest<>s.digest OR (c.queue_id<>s.queue_id AND s.queue_id<>'' AND c.queue_id<>'') OR (c.delivery_seq<>s.delivery_seq AND s.delivery_seq>0 AND c.delivery_seq>0)"); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_pending_batches(node_id,batch_id,digest,encrypted_batch,created_at,last_attempt_at,attempts,last_code,queue_id,delivery_seq)
 SELECT c.node_id,c.batch_id,c.digest,c.encrypted_batch,c.created_at,c.last_attempt_at,c.attempts,c.last_code,c.queue_id,c.delivery_seq
 FROM restore_source.traffic_pending_batches c
 WHERE NOT EXISTS(SELECT 1 FROM traffic_pending_batches s WHERE s.node_id=c.node_id AND s.batch_id=c.batch_id)`)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE traffic_pending_batches SET queue_id=c.queue_id,delivery_seq=c.delivery_seq FROM restore_source.traffic_pending_batches c WHERE c.node_id=traffic_pending_batches.node_id AND c.batch_id=traffic_pending_batches.batch_id AND traffic_pending_batches.delivery_seq=0 AND c.delivery_seq>0"); err != nil {
		return err
	}
	var count, size int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(length(encrypted_batch)),0) FROM traffic_pending_batches").Scan(&count, &size); err != nil {
		return err
	}
	if count > 10000 || size > 64<<20 {
		return db.ErrRecoveryConflict
	}
	return nil
}

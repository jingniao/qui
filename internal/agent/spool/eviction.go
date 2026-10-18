package spool

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"proxycontrol/pkg/contracts/access"
)

// Only authenticated allowed records may be sacrificed for capacity. Priority
// metadata alone cannot authorize deletion of an original damaged batch.
func (s *Spool) evictAllowedTx(ctx context.Context, tx *sql.Tx) (int64, int64, error) {
	var identity string
	var sealed []byte
	var count int64
	err := tx.QueryRowContext(ctx, "SELECT "+criticalTextSQL("id", 128)+",CASE WHEN typeof(records)='integer' THEN records ELSE -1 END,"+criticalPayloadSQL+" FROM batches WHERE blocked=0 ORDER BY rowid LIMIT 1", maxQueuedBatchBytes).Scan(&identity, &count, &sealed)
	if err != nil {
		return 0, 0, err
	}
	if count < 1 || count > access.MaxBatchRecords || len(sealed) == 0 {
		return 0, 0, ErrQueuedBatchInvalid
	}
	plain, err := s.vault.Open(sealed)
	if err != nil {
		return 0, 0, invalidQueuedBatch(err)
	}
	defer clear(plain)
	batch, err := access.DecodeBatch(plain)
	if err != nil || batch.NodeID != s.nodeID || batch.ID != identity || int64(len(batch.Records)) != count {
		return 0, 0, ErrQueuedBatchInvalid
	}
	for _, record := range batch.Records {
		if !record.Allowed {
			return 0, 0, ErrQueuedBatchInvalid
		}
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM batches WHERE id=? AND blocked=0 AND records=? AND payload=?", identity, count, sealed)
	if err != nil {
		return 0, 0, err
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return 0, 0, ErrQueuedBatchInvalid
	}
	return int64(len(sealed)), count, nil
}

func addEvictionCount(ctx context.Context, tx *sql.Tx, count int64) error {
	var previous int64
	if err := tx.QueryRowContext(ctx, "SELECT CASE WHEN typeof(dropped_allowed)='integer' THEN dropped_allowed ELSE -1 END FROM spool_metadata WHERE id=1").Scan(&previous); err != nil {
		return err
	}
	if previous < 0 || count < 0 || math.MaxInt64-previous < count {
		return errors.New("allowed rollup eviction counter invalid or exhausted")
	}
	result, err := tx.ExecContext(ctx, "UPDATE spool_metadata SET dropped_allowed=? WHERE id=1 AND dropped_allowed=?", previous+count, previous)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return ErrQueuedBatchInvalid
	}
	return nil
}

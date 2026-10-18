package spool

import (
	"context"
	"database/sql"
	"errors"
	"math"
)

// Native samples take priority over old allowed domain rollups. All evictions
// belong to the same transaction as every sample chunk and accounting cursor.
func (s *Spool) makeTrafficRoom(ctx context.Context, tx *sql.Tx, added int64) error {
	var used int64
	if err := tx.QueryRowContext(ctx, "SELECT "+queuedBytesSQL).Scan(&used); err != nil {
		return err
	}
	var dropped int64
	for used+added > s.limit {
		size, count, err := s.evictAllowedTx(ctx, tx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFull
		}
		if err != nil {
			return err
		}
		if used < size || math.MaxInt64-dropped < count {
			return errors.New("allowed rollup eviction overflow")
		}
		used -= size
		dropped += count
	}
	if dropped > 0 {
		return addEvictionCount(ctx, tx, dropped)
	}
	return nil
}

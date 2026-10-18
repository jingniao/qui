package spool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// Tiny logical limits are useful to callers; SQLite schema/index overhead has
// a separate minimum physical budget. Production defaults stay at 100 MiB.
func physicalLimit(logical int64) int64 { return max(logical, 4<<20) }
func configureDiskCapacity(ctx context.Context, database *sql.DB, logical int64) error {
	var pageSize, pageCount int64
	if err := database.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return err
	}
	if err := database.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return err
	}
	if pageSize < 512 || pageSize > 65536 || pageCount < 0 {
		return errors.New("invalid spool page geometry")
	}
	// Reserve most of the budget for rollback journaling and small sidecars.
	pages := (physicalLimit(logical) - (128 << 10)) / 3 / pageSize
	if pages < 1 || pageCount > pages {
		return errors.Join(ErrFull, errors.New("existing spool exceeds reserved database page budget; preserve original ledger"))
	}
	var mode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode=TRUNCATE").Scan(&mode); err != nil {
		return err
	}
	if mode != "truncate" {
		return errors.New("spool rollback journal mode unavailable")
	}
	if _, err := database.ExecContext(ctx, "PRAGMA journal_size_limit=0"); err != nil {
		return err
	}
	var applied int64
	if err := database.QueryRowContext(ctx, fmt.Sprintf("PRAGMA max_page_count=%d", pages)).Scan(&applied); err != nil {
		return err
	}
	if applied != pages {
		return errors.Join(ErrFull, errors.New("spool page capacity could not be applied"))
	}
	return nil
}
func (s *Spool) diskUsage() (int64, error) {
	var total int64
	for _, name := range []string{"queue.db", "queue.db-journal", "queue.db-wal", "queue.db-shm", "spool.key"} {
		info, err := os.Lstat(filepath.Join(filepath.Dir(s.path), name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if !info.Mode().IsRegular() || info.Size() < 0 || math.MaxInt64-total < info.Size() {
			return 0, errors.New("unsafe spool capacity file")
		}
		total += info.Size()
	}
	return total, nil
}
func (s *Spool) checkDiskCapacity() error {
	used, err := s.diskUsage()
	if err != nil {
		return err
	}
	if used > physicalLimit(s.limit) {
		return errors.Join(ErrFull, errors.New("spool database and sidecars exceed physical budget"))
	}
	return nil
}

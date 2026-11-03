package spool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
)

// Tiny logical limits are useful to callers; SQLite schema/index overhead has
// a separate minimum physical budget. Production defaults stay at 100 MiB.
func physicalLimit(logical int64) int64 { return max(logical, 4<<20) }

// capacityDSN carries the capacity pragmas in the DSN. SQLite keeps them per
// connection, and database/sql discards a connection whose statement was
// interrupted, so applying them once after opening left every replacement
// connection with SQLite's default 17.6 TiB page ceiling and an unbounded
// rollback journal. The pool therefore states them for each connection, and the
// page ceiling stays unknown (pages 0) only for the probe that measures it.
func capacityDSN(uri url.URL, pages int64) string {
	query := url.Values{}
	for _, pragma := range []string{"busy_timeout(5000)", "synchronous(FULL)", "wal_autocheckpoint(256)"} {
		query.Add("_pragma", pragma)
	}
	if pages > 0 {
		for _, pragma := range []string{"journal_mode(TRUNCATE)", "journal_size_limit(0)", fmt.Sprintf("max_page_count(%d)", pages)} {
			query.Add("_pragma", pragma)
		}
	}
	uri.RawQuery = query.Encode()
	return uri.String()
}

// planDiskCapacity measures the existing store over a probe connection and
// returns the page ceiling that every pooled connection must enforce.
func planDiskCapacity(ctx context.Context, uri string, logical int64) (int64, error) {
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		return 0, err
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
	return configureDiskCapacity(ctx, database, logical)
}

func configureDiskCapacity(ctx context.Context, database *sql.DB, logical int64) (int64, error) {
	var pageSize, pageCount int64
	if err := database.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, err
	}
	if err := database.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return 0, err
	}
	if pageSize < 512 || pageSize > 65536 || pageCount < 0 {
		return 0, errors.New("invalid spool page geometry")
	}
	// Reserve most of the budget for rollback journaling and small sidecars.
	pages := (physicalLimit(logical) - (128 << 10)) / 3 / pageSize
	if pages < 1 || pageCount > pages {
		return 0, errors.Join(ErrFull, errors.New("existing spool exceeds reserved database page budget; preserve original ledger"))
	}
	var mode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode=TRUNCATE").Scan(&mode); err != nil {
		return 0, err
	}
	if mode != "truncate" {
		return 0, errors.New("spool rollback journal mode unavailable")
	}
	if _, err := database.ExecContext(ctx, "PRAGMA journal_size_limit=0"); err != nil {
		return 0, err
	}
	var applied int64
	if err := database.QueryRowContext(ctx, fmt.Sprintf("PRAGMA max_page_count=%d", pages)).Scan(&applied); err != nil {
		return 0, err
	}
	if applied != pages {
		return 0, errors.Join(ErrFull, errors.New("spool page capacity could not be applied"))
	}
	return pages, nil
}

// verifyDiskCapacity confirms once, on the operational pool, that its
// connections really carry the ceiling its DSN requests.
func verifyDiskCapacity(ctx context.Context, database *sql.DB, pages int64) error {
	var mode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return err
	}
	if mode != "truncate" {
		return errors.New("spool rollback journal mode unavailable")
	}
	var applied int64
	if err := database.QueryRowContext(ctx, "PRAGMA max_page_count").Scan(&applied); err != nil {
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

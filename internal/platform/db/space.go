package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const ReclaimFreePageThreshold = 32
const reclaimSegmentPages = 256
const reclaimPassPages = 2048

// ReclaimFreePages is fixed SQLite housekeeping, not business-data cleanup.
// Existing NONE/FULL databases keep their format. Each pragma releases its
// write lock before the next segment; both page count and wall time are bounded.
func ReclaimFreePages(ctx context.Context, database *sql.DB) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	modeName := "unknown"
	conn, err := database.Conn(ctx)
	if err != nil {
		return modeName, false, err
	}
	defer conn.Close()
	var mode, free int64
	if err := conn.QueryRowContext(ctx, "PRAGMA main.auto_vacuum").Scan(&mode); err != nil {
		return modeName, false, err
	}
	if mode < 0 || mode > 2 {
		return modeName, false, errors.New("invalid SQLite space reclamation mode")
	}
	modeName = []string{"none", "full", "incremental"}[mode]
	if mode != 2 {
		return modeName, false, nil
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA main.freelist_count").Scan(&free); err != nil {
		return modeName, false, err
	}
	if free < 0 {
		return modeName, false, errors.New("invalid SQLite free page count")
	}
	for pages := 0; pages < reclaimPassPages && free > ReclaimFreePageThreshold; {
		segment := min(int64(reclaimSegmentPages), free-ReclaimFreePageThreshold)
		rows, err := conn.QueryContext(ctx, fmt.Sprintf("PRAGMA main.incremental_vacuum(%d)", segment))
		if err != nil {
			return modeName, false, err
		}
		// Consume the pragma fully; otherwise some drivers advance only one page.
		for rows.Next() {
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return modeName, false, err
		}
		pages += int(segment)
		if err := conn.QueryRowContext(ctx, "PRAGMA main.freelist_count").Scan(&free); err != nil {
			return modeName, false, err
		}
	}
	return modeName, free > ReclaimFreePageThreshold, nil
}

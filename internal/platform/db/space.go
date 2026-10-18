package db

import (
	"context"
	"database/sql"
	"errors"
)

// ReclaimFreePages is fixed SQLite housekeeping, not business-data cleanup.
// Existing NONE/FULL databases keep their format. Incremental mode reclaims at
// most 32 free pages; interrupted passes are safe to repeat without a ledger.
func ReclaimFreePages(ctx context.Context, database *sql.DB) (string, error) {
	modeName := "unknown"
	conn, err := database.Conn(ctx)
	if err != nil {
		return modeName, err
	}
	defer conn.Close()
	var mode, free int64
	if err := conn.QueryRowContext(ctx, "PRAGMA main.auto_vacuum").Scan(&mode); err != nil {
		return modeName, err
	}
	if mode < 0 || mode > 2 {
		return modeName, errors.New("invalid SQLite space reclamation mode")
	}
	modeName = []string{"none", "full", "incremental"}[mode]
	if mode != 2 {
		return modeName, nil
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA main.freelist_count").Scan(&free); err != nil {
		return modeName, err
	}
	if free < 0 {
		return modeName, errors.New("invalid SQLite free page count")
	}
	if free == 0 {
		return modeName, nil
	}
	rows, err := conn.QueryContext(ctx, "PRAGMA main.incremental_vacuum(32)")
	if err != nil {
		return modeName, err
	}
	// Consume the bounded pragma fully; some drivers otherwise advance just its
	// first page. SQLite owns the atomic page relocation and truncation.
	for rows.Next() {
	}
	return modeName, errors.Join(rows.Err(), rows.Close())
}

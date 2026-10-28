package backup

import (
	"context"
	"database/sql"
)

// Archives predating 0115 have no device limit; restore migrates them to zero.
func validateDeviceLimit(ctx context.Context, database *sql.DB) error {
	var applied int
	if database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0115_device_limit.sql'").Scan(&applied) != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	rows, err := database.QueryContext(ctx, "SELECT device_limit FROM users")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var n int
		if rows.Scan(&n) != nil || n < 0 || n > 100 {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}

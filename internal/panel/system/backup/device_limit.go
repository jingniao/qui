package backup

import (
	"context"
	"database/sql"
)

func validateDeviceLimit(ctx context.Context, database *sql.DB) error {
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

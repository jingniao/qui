package backup

import (
	"context"
	"database/sql"

	"proxycontrol/internal/panel/node"
)

// validateNodeTags checks the server-group tags added by 0144: every stored
// list must be exactly what the panel writes.
func validateNodeTags(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, "SELECT tags_json FROM node_meta")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if rows.Scan(&encoded) != nil {
			return ErrArchive
		}
		if _, err := node.DecodeTags(encoded); err != nil {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}

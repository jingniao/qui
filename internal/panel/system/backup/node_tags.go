package backup

import (
	"context"
	"database/sql"

	"proxycontrol/internal/panel/node"
)

// validateNodeTags checks the server-group tags added by 0144: every stored
// list must be exactly what the panel writes. An archive made before 0144 has
// no column and reads as untagged after the upgrade.
func validateNodeTags(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0144_node_tags.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
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

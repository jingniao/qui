package backup

import (
	"context"
	"database/sql"
	"strings"

	"proxycontrol/pkg/contracts/tuning"
)

func validateTuningDismissals(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0141_tuning_recommendations.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	rows, err := database.QueryContext(ctx, "SELECT d.key, d.value, n.id FROM node_tuning_dismissals d LEFT JOIN nodes n ON n.id=d.node_id")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		var nodeID sql.NullString
		if err := rows.Scan(&key, &value, &nodeID); err != nil {
			return ErrArchive
		}
		if !nodeID.Valid || !tuning.Known(key) || strings.TrimSpace(value) == "" || len(value) > 256 {
			return ErrArchive
		}
	}
	if err := rows.Err(); err != nil {
		return ErrArchive
	}
	return nil
}

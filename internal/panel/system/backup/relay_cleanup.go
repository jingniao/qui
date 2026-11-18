package backup

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"
)

func validateRelayCleanup(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT c.node_id,c.reason,c.attempts,c.last_code,c.next_attempt_at,c.created_at,c.updated_at,c.revision_id,n.id,r.id
FROM relay_cleanup_nodes c LEFT JOIN nodes n ON n.id=c.node_id LEFT JOIN config_revisions r ON r.id=c.revision_id`)
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var id, reason, code, next, created, updated string
		var nodeID *string
		var existingRevision *int64
		var attempts int64
		var revision *int64
		if rows.Scan(&id, &reason, &attempts, &code, &next, &created, &updated, &revision, &nodeID, &existingRevision) != nil || nodeID == nil || id == "" || strings.TrimSpace(reason) == "" || attempts < 0 || strings.ContainsFunc(code, unicode.IsControl) || (revision != nil && (*revision <= 0 || existingRevision == nil)) {
			return ErrArchive
		}
		for _, stamp := range []string{next, created, updated} {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				return ErrArchive
			}
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}

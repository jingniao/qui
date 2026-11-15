package backup

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"
)

func validateRelayCleanup(ctx context.Context, db *sql.DB) error {
	var applied, present int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0150_relay_cleanup.sql'").Scan(&applied) != nil ||
		db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('relay_cleanup_nodes','relay_cleanup_reconcile')").Scan(&present) != nil || present != applied*2 {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	var count, invalid int
	if db.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(id IS NULL OR id<>1 OR completed IS NULL OR completed NOT IN (0,1)),0) FROM relay_cleanup_reconcile").Scan(&count, &invalid) != nil || count != 1 || invalid != 0 {
		return ErrArchive
	}
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

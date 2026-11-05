package importer

import (
	"context"
	"database/sql"
)

// PrepareRestoreTx keeps current source definitions and grants authoritative.
// Replaying backup grants would restore a revoked external subscription. Caches
// are replaceable and are copied with their source, never from unrelated IDs.
//
// An archive from before 0133 has no result columns; that copy names the columns
// it has, so an old failure stays "no recorded cause" instead of gaining an
// invented one.
func (s *Store) PrepareRestoreTx(ctx context.Context, tx *sql.Tx) error {
	var results int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('subscription_sources','restore_source') WHERE name='latest_result_json'").Scan(&results); err != nil {
		return err
	}
	sources := "INSERT INTO subscription_sources(id,name,url_encrypted,interval_seconds,last_fetch_at,last_status,last_error,created_at,updated_at) SELECT id,name,url_encrypted,interval_seconds,last_fetch_at,last_status,last_error,created_at,updated_at FROM restore_source.subscription_sources"
	if results == 1 {
		sources = "INSERT INTO subscription_sources(id,name,url_encrypted,interval_seconds,last_fetch_at,last_success_at,last_status,last_error,latest_result_json,created_at,updated_at) SELECT id,name,url_encrypted,interval_seconds,last_fetch_at,last_success_at,last_status,last_error,latest_result_json,created_at,updated_at FROM restore_source.subscription_sources"
	}
	for _, q := range []string{
		"DELETE FROM user_source_grants", "DELETE FROM imported_proxies", "DELETE FROM subscription_sources",
		sources,
		"INSERT INTO imported_proxies SELECT * FROM restore_source.imported_proxies",
		"INSERT INTO user_source_grants SELECT g.* FROM restore_source.user_source_grants g JOIN users u ON u.id=g.user_id",
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

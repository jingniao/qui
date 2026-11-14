package importer

import (
	"context"
	"database/sql"
)

// PrepareImportIdentityRestoreTx keeps the marker, provenance and report from
// the same authoritative source as the restored imports. Pre-0147 current
// evidence has no identity state and must undergo the one-time conversion.
func (s *Store) PrepareImportIdentityRestoreTx(ctx context.Context, tx *sql.Tx) error {
	var applied int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.schema_migrations WHERE version='0147_imported_proxy_identity.sql'").Scan(&applied); err != nil {
		return err
	}
	for _, query := range []string{
		"DELETE FROM import_identity_map",
		"DELETE FROM import_identity_reconcile",
		"DELETE FROM system_settings WHERE key='import_identity_report'",
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	if applied == 0 {
		_, err := tx.ExecContext(ctx, "INSERT INTO import_identity_reconcile(id,done) VALUES(1,0)")
		return err
	}
	for _, query := range []string{
		"INSERT INTO import_identity_reconcile(id,done) SELECT id,done FROM restore_source.import_identity_reconcile",
		"INSERT INTO import_identity_map(source_id,old_proxy_id,old_proxy_key,new_proxy_id) SELECT source_id,old_proxy_id,old_proxy_key,new_proxy_id FROM restore_source.import_identity_map",
		"INSERT INTO system_settings(key,value_json) SELECT key,value_json FROM restore_source.system_settings WHERE key='import_identity_report'",
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

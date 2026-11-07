package recovery

import (
	"database/sql"
	"testing"
)

// rewindIdentityRecovery removes migration 0134 so a fixture can stand in for an
// archive written before it. That migration added the recovery and retirement
// tables and two enrollment-token columns; `purpose` carries a CHECK constraint
// that SQLite refuses to drop, so the token table is rebuilt to the shape
// 0003_nodes.sql created and the remaining rows are copied over.
func rewindIdentityRecovery(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0134_node_identity_recovery.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DROP TABLE node_identity_recoveries",
		"DROP TABLE node_identity_retirements",
		"CREATE TABLE node_enrollment_tokens_pre0134 (token_hash TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE, expires_at TEXT NOT NULL, consumed_at TEXT)",
		"INSERT INTO node_enrollment_tokens_pre0134(token_hash,node_id,expires_at,consumed_at) SELECT token_hash,node_id,expires_at,consumed_at FROM node_enrollment_tokens",
		"DROP TABLE node_enrollment_tokens",
		"ALTER TABLE node_enrollment_tokens_pre0134 RENAME TO node_enrollment_tokens",
		"DELETE FROM schema_migrations WHERE version='0134_node_identity_recovery.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

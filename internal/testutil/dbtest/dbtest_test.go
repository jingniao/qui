package dbtest

import (
	"context"
	"path/filepath"
	"testing"

	"proxycontrol/internal/platform/db"
)

// A database opened from the template has the same applied migrations as one
// db.Open migrated from scratch, and an existing file is never replaced.
func TestOpenMatchesFreshMigration(t *testing.T) {
	ctx := context.Background()
	fresh, err := db.Open(ctx, filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	path := filepath.Join(t.TempDir(), "nested", "panel.db")
	copied, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var freshList, copiedList string
	if err := fresh.QueryRowContext(ctx, "SELECT group_concat(version || ':' || checksum, ',') FROM (SELECT version, checksum FROM schema_migrations ORDER BY version)").Scan(&freshList); err != nil {
		t.Fatal(err)
	}
	if err := copied.QueryRowContext(ctx, "SELECT group_concat(version || ':' || checksum, ',') FROM (SELECT version, checksum FROM schema_migrations ORDER BY version)").Scan(&copiedList); err != nil {
		t.Fatal(err)
	}
	if freshList == "" || freshList != copiedList {
		t.Fatalf("template migrations differ:\n%s\n%s", freshList, copiedList)
	}
	if _, err := copied.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('dbtest-marker','1')"); err != nil {
		t.Fatal(err)
	}
	copied.Close()
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var value string
	if err := reopened.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='dbtest-marker'").Scan(&value); err != nil || value != "1" {
		t.Fatal("an existing database was replaced by the template", value, err)
	}
}

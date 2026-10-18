package db

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"proxycontrol/migrations"
)

func TestOpenMigratesAndPersists(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested/panel.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, pragma := range []struct {
		name string
		want int
	}{{"foreign_keys", 1}, {"busy_timeout", 5000}} {
		var got int
		if err := database.QueryRow("PRAGMA " + pragma.name).Scan(&got); err != nil || got != pragma.want {
			t.Fatalf("%s: got %d, err %v", pragma.name, got, err)
		}
	}
	var mode string
	if err := database.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode %q: %v", mode, err)
	}
	database.Close()
	database, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count int
	if err := database.QueryRow("SELECT count(*) FROM app_metadata").Scan(&count); err != nil || count != 1 {
		t.Fatalf("bootstrap repeated: %d %v", count, err)
	}
	if err := database.QueryRow("PRAGMA integrity_check").Scan(&mode); err != nil || mode != "ok" {
		t.Fatalf("integrity: %s %v", mode, err)
	}
}

func migrationFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	return files
}

func TestFailedMigrationRollsBackAndCanRetry(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	files := migrationFiles(t)
	files["9999_failure.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE should_not_exist(id INTEGER); INSERT INTO missing_table VALUES(1);")}
	if Migrate(ctx, database, files) == nil {
		t.Fatal("invalid migration succeeded")
	}
	var count int
	database.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'should_not_exist'").Scan(&count)
	if count != 0 {
		t.Fatal("failed migration leaked a table")
	}
	files["9999_failure.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE should_not_exist(id INTEGER);")}
	if err := Migrate(ctx, database, files); err != nil {
		t.Fatal(err)
	}
	files["9999_failure.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE changed(id INTEGER);")}
	if Migrate(ctx, database, files) == nil {
		t.Fatal("accepted modified applied migration")
	}
	delete(files, "9999_failure.sql")
	if Migrate(ctx, database, files) == nil {
		t.Fatal("accepted binary older than database")
	}
}

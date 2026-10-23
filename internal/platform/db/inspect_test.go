package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"testing/fstest"

	"proxycontrol/migrations"
)

func migrationSet(entries map[string]string) fstest.MapFS {
	files := fstest.MapFS{}
	for name, data := range entries {
		files[name] = &fstest.MapFile{Data: []byte(data)}
	}
	return files
}

// createDatabase migrates a fresh database with the given set and closes it, so
// callers hand Inspect a file that is finished and unlocked.
func createDatabase(t *testing.T, files fs.FS) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.db")
	uri, err := URI(path)
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if err := Migrate(context.Background(), database, files); err != nil {
		t.Fatal(err)
	}
	return path
}

// fingerprint identifies a file by size and content so a test can prove the
// inspection left the original untouched.
func fingerprint(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%d %x", info.Size(), sha256.Sum256(data))
}

func TestInspectRealMigrationsIsCompatibleAndLeavesFileAlone(t *testing.T) {
	path := createDatabase(t, migrations.Files)
	before := fingerprint(t, path)
	inspection := Inspect(context.Background(), path, migrations.Files)
	if inspection.Verdict != VerdictCompatible {
		t.Fatalf("verdict %s: %+v", inspection.Verdict, inspection)
	}
	if inspection.Integrity != "ok" {
		t.Fatalf("integrity %q", inspection.Integrity)
	}
	if len(inspection.Applied) == 0 || !sort.StringsAreSorted(inspection.Applied) {
		t.Fatalf("applied %v", inspection.Applied)
	}
	if len(inspection.Pending) != 0 || len(inspection.Unknown) != 0 || len(inspection.Mismatched) != 0 {
		t.Fatalf("unexpected drift: %+v", inspection)
	}
	if after := fingerprint(t, path); after != before {
		t.Fatalf("inspection changed the database: %s -> %s", before, after)
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Lstat(sidecar); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("inspection touched or needs %s: %v", sidecar, err)
		}
	}
}

func TestInspectTreatsNotYetAppliedMigrationAsCompatible(t *testing.T) {
	path := createDatabase(t, migrationSet(map[string]string{
		"a.sql": "CREATE TABLE a(id INTEGER);",
		"b.sql": "CREATE TABLE b(id INTEGER);",
	}))
	inspection := Inspect(context.Background(), path, migrationSet(map[string]string{
		"a.sql": "CREATE TABLE a(id INTEGER);",
		"b.sql": "CREATE TABLE b(id INTEGER);",
		"c.sql": "CREATE TABLE c(id INTEGER);",
	}))
	if inspection.Verdict != VerdictCompatible {
		t.Fatalf("verdict %s: %+v", inspection.Verdict, inspection)
	}
	if !reflect.DeepEqual(inspection.Applied, []string{"a.sql", "b.sql"}) {
		t.Fatalf("applied %v", inspection.Applied)
	}
	if !reflect.DeepEqual(inspection.Pending, []string{"c.sql"}) {
		t.Fatalf("pending %v", inspection.Pending)
	}
}

func TestInspectTreatsUnknownMigrationAsNewer(t *testing.T) {
	path := createDatabase(t, migrationSet(map[string]string{
		"a.sql": "CREATE TABLE a(id INTEGER);",
		"b.sql": "CREATE TABLE b(id INTEGER);",
		"c.sql": "CREATE TABLE c(id INTEGER);",
	}))
	inspection := Inspect(context.Background(), path, migrationSet(map[string]string{
		"a.sql": "CREATE TABLE a(id INTEGER);",
		"b.sql": "CREATE TABLE b(id INTEGER);",
	}))
	if inspection.Verdict != VerdictNewer {
		t.Fatalf("verdict %s: %+v", inspection.Verdict, inspection)
	}
	if !reflect.DeepEqual(inspection.Unknown, []string{"c.sql"}) {
		t.Fatalf("unknown %v", inspection.Unknown)
	}
	if len(inspection.Pending) != 0 || len(inspection.Mismatched) != 0 {
		t.Fatalf("unexpected drift: %+v", inspection)
	}
}

func TestInspectTreatsChecksumMismatchAsForeign(t *testing.T) {
	path := createDatabase(t, migrationSet(map[string]string{
		"a.sql": "CREATE TABLE a(id INTEGER);",
		"b.sql": "CREATE TABLE b(id INTEGER);",
	}))
	inspection := Inspect(context.Background(), path, migrationSet(map[string]string{
		"a.sql": "CREATE TABLE a(id INTEGER);",
		"b.sql": "CREATE TABLE b(id INTEGER, changed TEXT);",
	}))
	if inspection.Verdict != VerdictForeign {
		t.Fatalf("verdict %s: %+v", inspection.Verdict, inspection)
	}
	if !reflect.DeepEqual(inspection.Mismatched, []string{"b.sql"}) {
		t.Fatalf("mismatched %v", inspection.Mismatched)
	}
}

func TestInspectTreatsDatabaseWithoutMigrationTableAsForeign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	uri, err := URI(path)
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("CREATE TABLE unrelated(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	inspection := Inspect(context.Background(), path, migrationSet(map[string]string{
		"a.sql": "CREATE TABLE a(id INTEGER);",
	}))
	if inspection.Verdict != VerdictForeign {
		t.Fatalf("verdict %s: %+v", inspection.Verdict, inspection)
	}
	if len(inspection.Applied) != 0 || inspection.Integrity != "ok" {
		t.Fatalf("unexpected report: %+v", inspection)
	}
}

func TestInspectTreatsUnreadableTargetsAsDamaged(t *testing.T) {
	files := migrationSet(map[string]string{"a.sql": "CREATE TABLE a(id INTEGER);"})
	valid := createDatabase(t, files)
	truncated := filepath.Join(t.TempDir(), "truncated.db")
	original, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(truncated, original[:100], 0600); err != nil {
		t.Fatal(err)
	}
	garbage := filepath.Join(t.TempDir(), "garbage.db")
	if err := os.WriteFile(garbage, []byte("this is not a SQLite database at all"), 0600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"garbage":   garbage,
		"truncated": truncated,
		"directory": t.TempDir(),
		"missing":   filepath.Join(t.TempDir(), "missing.db"),
	}
	symlink := filepath.Join(t.TempDir(), "symlink.db")
	if err := os.Symlink(valid, symlink); err != nil {
		t.Logf("skipping the symlink target: %v", err)
	} else {
		cases["symlink"] = symlink
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			inspection := Inspect(context.Background(), path, files)
			if inspection.Verdict != VerdictDamaged {
				t.Fatalf("verdict %s: %+v", inspection.Verdict, inspection)
			}
			if inspection.Integrity == "" || inspection.Integrity == "ok" {
				t.Fatalf("damaged file without a reason: %+v", inspection)
			}
		})
	}
}

func TestInspectReadsWriteAheadLogSidecar(t *testing.T) {
	ctx := context.Background()
	files := migrationSet(map[string]string{
		"a.sql": "CREATE TABLE a(id INTEGER);",
		"b.sql": "CREATE TABLE b(id INTEGER);",
	})
	path := filepath.Join(t.TempDir(), "panel.db")
	uri, err := URI(path)
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	// Keep every committed page in the log so a copy without it is incomplete.
	for _, pragma := range []string{"PRAGMA journal_mode = WAL", "PRAGMA wal_autocheckpoint = 0"} {
		if _, err := database.ExecContext(ctx, pragma); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, database, files); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("expected a populated write-ahead log: %v", err)
	}
	copied := t.TempDir()
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(copied, "panel.db"+suffix), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	inspection := Inspect(ctx, filepath.Join(copied, "panel.db"), files)
	if inspection.Verdict != VerdictCompatible {
		t.Fatalf("verdict %s: %+v", inspection.Verdict, inspection)
	}
	if !reflect.DeepEqual(inspection.Applied, []string{"a.sql", "b.sql"}) {
		t.Fatalf("applied %v", inspection.Applied)
	}
}

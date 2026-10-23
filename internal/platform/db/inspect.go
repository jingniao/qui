package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

// Verdict summarises whether this binary can open a stopped panel database.
type Verdict string

const (
	// VerdictCompatible means every applied migration is known here with a
	// matching checksum; migrations still pending would be applied on startup.
	VerdictCompatible Verdict = "compatible"
	// VerdictNewer means at least one applied migration is unknown here, so the
	// database was written by a newer panel.
	VerdictNewer Verdict = "newer"
	// VerdictForeign means the file has no migration table, or a migration this
	// binary knows carries a different checksum.
	VerdictForeign Verdict = "foreign"
	// VerdictDamaged means the file cannot be copied, opened, read as SQLite or
	// fails its own integrity check.
	VerdictDamaged Verdict = "damaged"
)

// Inspection is the report of a read-only database compatibility check.
type Inspection struct {
	Verdict    Verdict
	Applied    []string
	Pending    []string
	Unknown    []string
	Mismatched []string
	// Integrity is "ok" or the first integrity_check message; for a damaged
	// file it carries the error that stopped the inspection.
	Integrity string
}

// Inspect reports whether this binary can open the panel database at path. It
// never opens the original: the file and its write-ahead log are copied into a
// private directory first, so the check never touches the locks, sidecars or
// bytes of the file it reports on. The file should belong to a stopped panel:
// a copy taken while a panel writes is not guaranteed to be consistent.
func Inspect(ctx context.Context, path string, files fs.FS) Inspection {
	copied, cleanup, err := copyDatabase(path)
	if err != nil {
		return damaged(err)
	}
	defer cleanup()
	uri, err := URI(copied)
	if err != nil {
		return damaged(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		return damaged(err)
	}
	// The copy is opened read-write because SQLite replays the copied log into
	// it, but the copy is private and removed again.
	database.SetMaxOpenConns(1)
	defer database.Close()
	return inspect(ctx, database, files)
}

func inspect(ctx context.Context, database *sql.DB, files fs.FS) Inspection {
	message, ok, err := integrityCheck(ctx, database)
	if err != nil {
		return damaged(err)
	}
	if !ok {
		return Inspection{Verdict: VerdictDamaged, Integrity: message}
	}
	checksums, names, err := embeddedMigrations(files)
	if err != nil {
		return damaged(err)
	}
	var tables int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'").Scan(&tables); err != nil {
		return damaged(err)
	}
	applied := map[string]string{}
	if tables > 0 {
		rows, err := database.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
		if err != nil {
			return damaged(err)
		}
		for rows.Next() {
			var version, checksum string
			if err := rows.Scan(&version, &checksum); err != nil {
				rows.Close()
				return damaged(err)
			}
			applied[version] = checksum
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return damaged(err)
		}
	}
	inspection := Inspection{Integrity: "ok"}
	for version := range applied {
		inspection.Applied = append(inspection.Applied, version)
	}
	sort.Strings(inspection.Applied)
	for _, name := range names {
		checksum, known := applied[name]
		switch {
		case !known:
			inspection.Pending = append(inspection.Pending, name)
		case checksum != checksums[name]:
			// Applied migrations are immutable, so a different checksum means
			// this is not a database of this product or it was edited by hand.
			inspection.Mismatched = append(inspection.Mismatched, name)
		}
	}
	for _, version := range inspection.Applied {
		if _, known := checksums[version]; !known {
			inspection.Unknown = append(inspection.Unknown, version)
		}
	}
	switch {
	case tables == 0 || len(inspection.Mismatched) > 0:
		inspection.Verdict = VerdictForeign
	case len(inspection.Unknown) > 0:
		inspection.Verdict = VerdictNewer
	default:
		inspection.Verdict = VerdictCompatible
	}
	return inspection
}

// copyDatabase clones the database and its write-ahead log into a new private
// directory and returns the clone path with the cleanup for that directory.
func copyDatabase(path string) (string, func(), error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", nil, fmt.Errorf("inspect database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("inspect database: %s is not a regular file", path)
	}
	directory, err := os.MkdirTemp("", "proxy-panel-check-*")
	if err != nil {
		return "", nil, fmt.Errorf("inspect database: %w", err)
	}
	cleanup := func() { os.RemoveAll(directory) }
	target := filepath.Join(directory, "panel.db")
	if err := copyFile(path, target); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("inspect database: %w", err)
	}
	// Ignore the shared-memory index: SQLite rebuilds it from the log itself.
	if sidecar, err := os.Lstat(path + "-wal"); err == nil {
		if !sidecar.Mode().IsRegular() {
			cleanup()
			return "", nil, fmt.Errorf("inspect database: %s-wal is not a regular file", path)
		}
		if err := copyFile(path+"-wal", target+"-wal"); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("inspect database: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		cleanup()
		return "", nil, fmt.Errorf("inspect database: %w", err)
	}
	return target, cleanup, nil
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

// integrityCheck returns the first report row and whether the whole check
// produced exactly "ok".
func integrityCheck(ctx context.Context, database *sql.DB) (string, bool, error) {
	rows, err := database.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	var messages []string
	for rows.Next() {
		var message string
		if err := rows.Scan(&message); err != nil {
			return "", false, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if len(messages) == 0 {
		return "integrity_check returned no rows", false, nil
	}
	return messages[0], len(messages) == 1 && messages[0] == "ok", nil
}

// embeddedMigrations reads the same names, order and checksum definition that
// Migrate uses, so the comparison is the one startup would perform.
func embeddedMigrations(files fs.FS) (map[string]string, []string, error) {
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(names)
	checksums := make(map[string]string, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, nil, err
		}
		checksums[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return checksums, names, nil
}

func damaged(err error) Inspection {
	return Inspection{Verdict: VerdictDamaged, Integrity: err.Error()}
}

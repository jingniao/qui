package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	// VerdictOutdated means the database has not reached v1.7.5.
	// Upgrade it through v1.7.5 first (ErrPredatesBaseline).
	VerdictOutdated Verdict = "outdated"
	// VerdictDamaged means the file cannot be opened, read as SQLite or
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

// Inspect checks an offline database and its WAL without copying or writing
// either file. The caller must exclude writers for the entire inspection.
// Private hardlinks keep SQLite's transient SHM index away from the backup.
// The database and WAL are opened read-only and never checkpointed or copied.
func Inspect(ctx context.Context, path string, files fs.FS) (result Inspection) {
	for _, suffix := range []string{"", "-wal", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if suffix != "" && errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return damaged(fmt.Errorf("inspect database: %w", err))
		}
		if !info.Mode().IsRegular() {
			return damaged(fmt.Errorf("inspect database: %s is not a regular file", path+suffix))
		}
		// Do not allow recovery to write through the private hardlinks.
		if suffix == "-journal" && info.Size() != 0 {
			return damaged(errors.New("inspect database: nonempty rollback journal requires recovery"))
		}
	}
	directory, err := os.MkdirTemp(filepath.Dir(path), ".proxy-panel-check-*")
	if err != nil {
		return damaged(fmt.Errorf("inspect database: %w", err))
	}
	defer func() {
		if err := os.RemoveAll(directory); err != nil {
			result = damaged(fmt.Errorf("inspect database: remove private index directory: %w", err))
		}
	}()
	linked := filepath.Join(directory, "panel.db")
	for _, suffix := range []string{"", "-wal"} {
		if err := os.Link(path+suffix, linked+suffix); err != nil {
			if suffix != "" && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return damaged(fmt.Errorf("inspect database: link offline input: %w", err))
		}
	}
	uri, err := fileURI(linked, "ro", []string{"foreign_keys(1)", "busy_timeout(5000)", "synchronous(NORMAL)", "query_only(1)"})
	if err != nil {
		return damaged(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		return damaged(err)
	}
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
	checksums, names, err := embeddedChecksums(files)
	if err != nil {
		return damaged(err)
	}
	var tables int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'").Scan(&tables); err != nil {
		return damaged(err)
	}
	recorded := map[string]string{}
	if tables > 0 {
		if recorded, err = recordedMigrations(ctx, database); err != nil {
			return damaged(err)
		}
	}
	// Applied migrations are immutable, so a different checksum means this is
	// not a database of this product or it was edited by hand.
	state := classifyMigrations(recorded, checksums)
	inspection := Inspection{Integrity: "ok", Unknown: state.unknown, Mismatched: state.mismatched}
	for version := range recorded {
		inspection.Applied = append(inspection.Applied, version)
	}
	sort.Strings(inspection.Applied)
	for _, name := range names {
		if !state.applied[name] {
			inspection.Pending = append(inspection.Pending, name)
		}
	}
	switch {
	case tables == 0 || len(inspection.Mismatched) > 0:
		inspection.Verdict = VerdictForeign
	case state.outdated:
		inspection.Verdict = VerdictOutdated
	case len(inspection.Unknown) > 0:
		inspection.Verdict = VerdictNewer
	default:
		inspection.Verdict = VerdictCompatible
	}
	return inspection
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

func damaged(err error) Inspection {
	return Inspection{Verdict: VerdictDamaged, Integrity: err.Error()}
}

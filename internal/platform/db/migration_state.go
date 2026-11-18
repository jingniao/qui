package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
)

// BaselineMigration creates the schema and built-in rows that the legacy
// migrations 0001-0152 produced (v1.6.0). A database that recorded the whole
// legacy chain counts as having applied it; its rows are kept as they are.
const BaselineMigration = "0152_baseline.sql"

// ErrPredatesBaseline is returned for a database that recorded only part of
// the legacy chain: it was last opened by a panel older than v1.6.0.
var ErrPredatesBaseline = errors.New("database predates v1.6.0; upgrade to v1.6.0 and start it once before installing this version")

// migrationState compares the schema_migrations rows of one database with the
// migrations a binary carries.
type migrationState struct {
	// applied holds the migrations this binary treats as applied: the recorded
	// rows it knows, plus BaselineMigration for a complete legacy chain.
	applied    map[string]bool
	unknown    []string
	mismatched []string
	outdated   bool
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func embeddedChecksums(files fs.FS) (map[string]string, []string, error) {
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

func recordedMigrations(ctx context.Context, q queryer) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	recorded := map[string]string{}
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, err
		}
		recorded[version] = checksum
	}
	return recorded, rows.Err()
}

func classifyMigrations(recorded, checksums map[string]string) migrationState {
	state := migrationState{applied: map[string]bool{}}
	legacy := 0
	for version, checksum := range recorded {
		if want, ok := legacyMigrations[version]; ok {
			if checksum != want {
				state.mismatched = append(state.mismatched, version)
			}
			legacy++
			continue
		}
		want, ok := checksums[version]
		switch {
		case !ok:
			state.unknown = append(state.unknown, version)
		case checksum != want:
			state.mismatched = append(state.mismatched, version)
		default:
			state.applied[version] = true
		}
	}
	if legacy > 0 {
		if legacy == len(legacyMigrations) {
			state.applied[BaselineMigration] = true
		} else {
			state.outdated = true
		}
	}
	sort.Strings(state.unknown)
	sort.Strings(state.mismatched)
	return state
}

// ValidateMigrationPrefix checks that a database (a backup archive) applied an
// unbroken prefix of files with matching checksums, starting at the baseline.
// Databases from before v1.6.0 return ErrPredatesBaseline.
func ValidateMigrationPrefix(ctx context.Context, q queryer, files fs.FS) error {
	checksums, names, err := embeddedChecksums(files)
	if err != nil {
		return err
	}
	recorded, err := recordedMigrations(ctx, q)
	if err != nil {
		return err
	}
	state := classifyMigrations(recorded, checksums)
	switch {
	case len(state.mismatched) > 0:
		return fmt.Errorf("migration %s checksum mismatch", state.mismatched[0])
	case state.outdated:
		return ErrPredatesBaseline
	case len(state.unknown) > 0:
		return fmt.Errorf("database migration %s is newer than or absent from this binary", state.unknown[0])
	}
	count := 0
	for _, name := range names {
		if !state.applied[name] {
			break
		}
		count++
	}
	if count == 0 || count != len(state.applied) {
		return errors.New("applied migrations are not a prefix of this binary's migrations")
	}
	return nil
}

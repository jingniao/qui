// Package dbtest opens panel test databases from a migrated template.
//
// Running all migrations on an empty SQLite file takes about 0.15 s, but
// about 4.6 s under -race (modernc.org/sqlite is C translated to Go and the
// race detector instruments every memory access). Most tests only need a
// freshly migrated database, so Open copies a template that was migrated once
// and lets db.Open verify it: an already-migrated database only has its
// migration checksums compared, which takes milliseconds.
//
// Tests whose subject is migrating or opening a database (internal/platform/db,
// migrations, upgrade and restore paths that start from an empty or older
// file) keep calling db.Open directly.
package dbtest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"
)

var (
	templateOnce sync.Once
	templatePath string
	templateErr  error
)

// Open behaves like db.Open. When path does not exist yet it is first filled
// with a copy of the migrated template, so the result is the same database
// db.Open would have created, without running the migrations again. An
// existing file is opened unchanged.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		template, err := migratedTemplate(ctx)
		if err != nil {
			return nil, err
		}
		if err := copyFile(template, path); err != nil {
			return nil, err
		}
	}
	return db.Open(ctx, path)
}

// migratedTemplate returns a migrated database file shared by every test
// process. It is named after the migrations it contains, so a changed or added
// migration produces a new template, and it is published by rename, so
// concurrent package test processes never read a half-written file.
func migratedTemplate(ctx context.Context) (string, error) {
	templateOnce.Do(func() {
		var digest string
		digest, templateErr = migrationsDigest()
		if templateErr != nil {
			return
		}
		templatePath = filepath.Join(os.TempDir(), "qui-dbtest-"+digest[:16]+".db")
		if info, err := os.Stat(templatePath); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return
		}
		templateErr = buildTemplate(ctx, templatePath)
	})
	return templatePath, templateErr
}

func migrationsDigest() (string, error) {
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", name, len(data))
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func buildTemplate(ctx context.Context, path string) error {
	directory, err := os.MkdirTemp(filepath.Dir(path), "qui-dbtest-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	built := filepath.Join(directory, "panel.db")
	database, err := db.Open(ctx, built)
	if err != nil {
		return err
	}
	// Fold the WAL into the main file so a copy of that one file is complete.
	if _, err := database.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		database.Close()
		return err
	}
	if err := database.Close(); err != nil {
		return err
	}
	staged := filepath.Join(directory, "template.db")
	if err := copyFile(built, staged); err != nil {
		return err
	}
	if err := os.Rename(staged, path); err != nil {
		// Another test process published the same template first.
		if info, statErr := os.Stat(path); statErr == nil && info.Size() > 0 {
			return nil
		}
		return err
	}
	return nil
}

func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o600)
}

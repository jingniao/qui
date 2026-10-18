package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"proxycontrol/migrations"
)

// URI returns the file URI of a SQLite database path with the pragmas the
// panel relies on. Writes stay serialized because Open keeps a single
// connection.
func URI(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	query := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "synchronous(NORMAL)"} {
		query.Add("_pragma", pragma)
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}
	return uri.String(), nil
}

func Open(ctx context.Context, path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}
	uri, err := URI(absolute)
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if _, err = database.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err == nil {
		err = Migrate(ctx, database, migrations.Files)
	}
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("initialize SQLite: %w", err)
	}
	if err := os.Chmod(absolute, 0600); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func Migrate(ctx context.Context, database *sql.DB, files fs.FS) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
 version TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TEXT NOT NULL
 )`); err != nil {
		return err
	}
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	// Serialize competing process migrations, not only goroutines in this process.
	conn, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[name] = true
	}
	rows, err := conn.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return err
		}
		if !known[version] {
			rows.Close()
			return fmt.Errorf("database migration %s is newer than or absent from this binary", version)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range names {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(data))
		var stored string
		err = conn.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version = ?", name).Scan(&stored)
		if err == nil {
			if stored != checksum {
				return fmt.Errorf("migration %s checksum mismatch; applied migrations are immutable", name)
			}
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		if strings.TrimSpace(string(data)) == "" {
			return fmt.Errorf("empty migration %s", name)
		}
		if _, err = conn.ExecContext(ctx, string(data)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err = conn.ExecContext(ctx, "INSERT INTO schema_migrations(version, checksum, applied_at) VALUES(?, ?, ?)", name, checksum, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

package db

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/filelock"
	"strings"
)

// OpenExistingReadOnly does not create a database, migrate tables, adopt an
// empty database, or change journal mode. Existing WAL is included in reads;
// SQLite may manage shared metadata. Caller owns the panel lifetime locks.
func OpenExistingReadOnly(path string) (*sql.DB, error) {
	return openExistingCA(path, false)
}
func OpenExistingCACommit(path string) (*sql.DB, error) { return openExistingCA(path, true) }
func openExistingCA(path string, writable bool) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, errors.New("existing regular SQLite database required")
	}
	f, err := os.Open(absolute)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || filelock.CheckSingleLink(f) != nil {
		f.Close()
		return nil, errors.New("SQLite database changed or has aliases")
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	query := url.Values{"mode": []string{"ro"}, "_pragma": []string{"query_only(1)", "busy_timeout(5000)"}}
	if writable {
		query = url.Values{"mode": []string{"rw"}, "_pragma": []string{"synchronous(FULL)", "foreign_keys(1)", "busy_timeout(5000)"}}
	}
	uri.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	return database, nil
}

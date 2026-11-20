package db

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
)

// ReadPoolConnections is the number of connections in the read-only pool. The
// panel keeps exactly one writer connection, so a few readers are enough for
// the small user base this panel serves.
const ReadPoolConnections = 4

// OpenReadPool opens a second, read-only pool on an existing WAL database. It
// never migrates, writes or checkpoints: the writer pool created by Open stays
// the only writer. Reads see the committed data as of the start of their
// snapshot. Callers must use the writer for read-then-write paths.
//
// The pool keeps up to two idle connections to reuse their SQLite setup across
// reads. The owner must close it before replacing the database files.
func OpenReadPool(ctx context.Context, path string, connections int) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	query := url.Values{}
	query.Set("mode", "ro")
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "query_only(1)"} {
		query.Add("_pragma", pragma)
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	if connections < 1 {
		connections = 1
	}
	database.SetMaxOpenConns(connections)
	database.SetMaxIdleConns(min(2, connections))
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

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
// the only writer. Queries through this pool may miss a write that committed a
// moment ago, so callers must use it only for reads that tolerate a stale
// snapshot, never on a read-then-write path.
//
// The pool holds no idle connections: a read opens its own connection and
// releases it, so the panel keeps no file handle open between queries. The
// point of the pool is that a read never waits for the single writer
// connection, not connection reuse, and the panel's read volume is small.
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
	database.SetMaxIdleConns(0)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

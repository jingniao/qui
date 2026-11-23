package db

import (
	"context"
	"database/sql"
	"fmt"
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
	if connections < 1 {
		return nil, fmt.Errorf("read-only pool connections must be positive: %d", connections)
	}
	uri, err := fileURI(path, "ro", []string{"foreign_keys(1)", "busy_timeout(5000)", "query_only(1)"})
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(connections)
	database.SetMaxIdleConns(min(2, connections))
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

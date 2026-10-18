package db

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// WriteFreeze holds an IMMEDIATE SQLite transaction on a separate connection.
// It excludes all database writers while snapshot readers remain available.
// The transaction performs no data writes and is always rolled back.
type WriteFreeze struct {
	database   *sql.DB
	connection *sql.Conn
}

func FreezeForRecovery(ctx context.Context, path string) (*WriteFreeze, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrRecoveryConflict
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: url.Values{"mode": {"rw"}, "_pragma": {"busy_timeout(0)"}}.Encode()}
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	connection, err := database.Conn(ctx)
	if err != nil {
		database.Close()
		return nil, err
	}
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		connection.Close()
		database.Close()
		return nil, err
	}
	return &WriteFreeze{database: database, connection: connection}, nil
}

func (f *WriteFreeze) Close() error {
	if f == nil || f.connection == nil {
		return nil
	}
	_, err := f.connection.ExecContext(context.Background(), "ROLLBACK")
	err = errors.Join(err, f.connection.Close(), f.database.Close())
	f.connection = nil
	f.database = nil
	return err
}

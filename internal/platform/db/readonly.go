package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"modernc.org/sqlite"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
)

// OpenImmutable reads a checkpointed offline SQLite file without migrations,
// sidecars or data writes. The caller must exclude writers for its lifetime.
func OpenImmutable(ctx context.Context, path string) (*sql.DB, error) {
	return openPrepared(ctx, path, true)
}

// OpenPrepared holds an existing RW-capable connection in query_only mode.
// Recovery can construct its entire service graph before commitment, then
// enable writes on this same connection after the authenticated journal commits.
func OpenPrepared(ctx context.Context, path string) (*PreparedDatabase, error) {
	dsn, err := preparedDSN(path, false)
	if err != nil {
		return nil, err
	}
	connector := &preparedConnector{dsn: dsn, driver: &sqlite.Driver{}}
	database := sql.OpenDB(connector)
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return &PreparedDatabase{Database: database, connector: connector}, nil
}

type PreparedDatabase struct {
	Database  *sql.DB
	connector *preparedConnector
}

func (p *PreparedDatabase) EnableWrites(ctx context.Context) error {
	p.connector.mu.Lock()
	p.connector.writable = true
	p.connector.mu.Unlock()
	if _, err := p.Database.ExecContext(ctx, "PRAGMA query_only=OFF"); err != nil {
		p.connector.mu.Lock()
		p.connector.writable = false
		p.connector.mu.Unlock()
		return err
	}
	return nil
}
func (p *PreparedDatabase) Close() error {
	if p == nil {
		return nil
	}
	return p.Database.Close()
}

type preparedConnector struct {
	dsn      string
	driver   *sqlite.Driver
	mu       sync.Mutex
	writable bool
}

func (c *preparedConnector) Driver() driver.Driver { return c.driver }
func (c *preparedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	connection, err := c.driver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	writable := c.writable
	c.mu.Unlock()
	if !writable {
		executor, ok := connection.(driver.ExecerContext)
		if !ok {
			connection.Close()
			return nil, errors.New("SQLite driver lacks prepared connection controls")
		}
		if _, err := executor.ExecContext(ctx, "PRAGMA query_only=ON", nil); err != nil {
			connection.Close()
			return nil, err
		}
	}
	return connection, nil
}

func openPrepared(ctx context.Context, path string, immutable bool) (*sql.DB, error) {
	dsn, err := preparedDSN(path, immutable)
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func preparedDSN(path string, immutable bool) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	query := url.Values{"mode": {"rw"}, "_pragma": {"foreign_keys(1)", "busy_timeout(5000)"}}
	if immutable {
		query.Set("mode", "ro")
		query.Set("immutable", "1")
		query.Add("_pragma", "query_only(1)")
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}
	return uri.String(), nil
}

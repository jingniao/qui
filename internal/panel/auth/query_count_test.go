package auth_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"
)

// countingDriver counts the statements a connection executes. The panel's auth
// path must not spend more than a session lookup per authenticated request, and
// this wrapper is how the test proves it instead of trusting the code.
type countingDriver struct {
	inner   driver.Driver
	counter *int64
}

func (d *countingDriver) Open(name string) (driver.Conn, error) {
	connection, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: connection, counter: d.counter}, nil
}

type countingConn struct {
	driver.Conn
	counter *int64
}

func (c *countingConn) count() { atomic.AddInt64(c.counter, 1) }

func (c *countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.count()
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return queryer.QueryContext(ctx, query, args)
}

func (c *countingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.count()
	executor, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return executor.ExecContext(ctx, query, args)
}

func (c *countingConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	beginner, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	return beginner.BeginTx(ctx, options)
}

func (c *countingConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *countingConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (c *countingConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

type countingConnector struct {
	dsn    string
	driver *countingDriver
}

func (c *countingConnector) Connect(context.Context) (driver.Conn, error) {
	return c.driver.Open(c.dsn)
}
func (c *countingConnector) Driver() driver.Driver { return c.driver }

// countingFixture builds the auth handler on a database whose statements are
// counted. The reader is left unset so every statement of a request is counted.
func countingFixture(t *testing.T) (*auth.Service, http.Handler, *int64) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	uri, err := db.URI(path)
	if err != nil {
		t.Fatal(err)
	}
	counter := new(int64)
	database := sql.OpenDB(&countingConnector{dsn: uri, driver: &countingDriver{inner: &sqlite.Driver{}, counter: counter}})
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	settings := &system.Repository{Database: database}
	service := &auth.Service{Repository: &auth.Repository{Database: database}, Audit: settings, Settings: settings, SessionDuration: 2 * time.Hour}
	installSetupToken(t, service)
	handler := &auth.Handler{Service: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	handler.Register(mux)
	return service, mux, counter
}

// TestAuthenticatedRequestQueryCount keeps the per-request query budget: the
// first-run and base-setting state is cached, and a session is one statement.
func TestAuthenticatedRequestQueryCount(t *testing.T) {
	_, mux, counter := countingFixture(t)
	cookie, _ := setup(t, mux)

	measure := func(method, path string) int64 {
		t.Helper()
		atomic.StoreInt64(counter, 0)
		response := send(t, mux, method, path, nil, cookie, "", "")
		mustStatus(t, response, 200)
		statements := atomic.LoadInt64(counter)
		t.Logf("%s %s: %d statements", method, path, statements)
		return statements
	}

	// The setup response already primed the cache; this request is steady state.
	if statements := measure("GET", "/api/v1/auth/session"); statements > 2 {
		t.Fatalf("authenticated session request ran %d statements, want at most 2", statements)
	}
	// A protected list request adds the endpoint's own count and page queries.
	if statements := measure("GET", "/api/v1/admins"); statements > 3 {
		t.Fatalf("protected list request ran %d statements, want at most 3", statements)
	}
}

// TestSetupStateCacheSurvivesInvalidation checks that a dropped cache reloads
// the same answer instead of a default one.
func TestSetupStateCacheSurvivesInvalidation(t *testing.T) {
	service, mux, counter := countingFixture(t)
	cookie, session := setup(t, mux)
	service.InvalidateSetupState()
	atomic.StoreInt64(counter, 0)
	response := send(t, mux, "GET", "/api/v1/auth/session", nil, cookie, "", "")
	mustStatus(t, response, 200)
	reloaded := readSession(t, response)
	if reloaded.PlatformName != session.PlatformName || reloaded.Timezone != session.Timezone || !reloaded.IsInitialized {
		t.Fatalf("cold cache lost the setup state: %+v", reloaded)
	}
	if statements := atomic.LoadInt64(counter); statements > 3 {
		t.Fatalf("cold session request ran %d statements, want at most 3", statements)
	}
	atomic.StoreInt64(counter, 0)
	response = send(t, mux, "GET", "/api/v1/auth/session", nil, cookie, "", "")
	mustStatus(t, response, 200)
	if statements := atomic.LoadInt64(counter); statements > 2 {
		t.Fatalf("warm session request ran %d statements, want at most 2", statements)
	}
}

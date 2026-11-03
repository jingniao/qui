package spool

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	sqlite3 "modernc.org/sqlite"
	libsqlite3 "modernc.org/sqlite/lib"
)

// A cancelled observation makes database/sql discard the pooled connection, and
// SQLite keeps the capacity pragmas per connection. The replacement must come
// back with the same page ceiling: without the DSN pragmas it reports SQLite's
// 17.6 TiB default, the panel rejects the observation and the spool stops
// enforcing its size cap (production, 2026-11-03).
func TestSpoolDiskCapacitySurvivesConnectionReplacement(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir(), "node", 512)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.DatabaseLimit < 1 || before.DatabaseLimit > before.DiskLimit {
		t.Fatalf("initial database ceiling out of budget: %+v", before)
	}
	// The physical connection is discarded exactly as an interrupted statement
	// discards it; every later query runs on a new one.
	conn, err := s.database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("pooled connection not discarded: %v", err)
	}
	if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
		t.Fatal(err)
	}
	after, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.DatabaseLimit != before.DatabaseLimit || after.DatabaseLimit > after.DiskLimit {
		t.Fatalf("replacement connection lost the page ceiling: before %+v after %+v", before, after)
	}
	var mode string
	var journalLimit int64
	if err := s.database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if err := s.database.QueryRowContext(ctx, "PRAGMA journal_size_limit").Scan(&journalLimit); err != nil {
		t.Fatal(err)
	}
	if mode != "truncate" || journalLimit != 0 {
		t.Fatalf("replacement connection lost its rollback journal settings: mode=%q limit=%d", mode, journalLimit)
	}
	// The ceiling is enforced, not only reported: a disposable table that
	// outgrows the reserved page budget fails with SQLITE_FULL.
	if _, err := s.database.ExecContext(ctx, "CREATE TABLE capacity_probe(payload BLOB NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 8192)
	var writeErr error
	for attempt := 0; attempt < 4096 && writeErr == nil; attempt++ {
		_, writeErr = s.database.ExecContext(ctx, "INSERT INTO capacity_probe(payload) VALUES(?)", payload)
	}
	var sqliteErr *sqlite3.Error
	if !errors.As(writeErr, &sqliteErr) || sqliteErr.Code() != libsqlite3.SQLITE_FULL {
		t.Fatalf("growth past the page budget was not refused with SQLITE_FULL: %v", writeErr)
	}
	var pageSize, pageCount, maxPages int64
	for _, query := range []struct {
		sql string
		dst *int64
	}{{"PRAGMA page_size", &pageSize}, {"PRAGMA page_count", &pageCount}, {"PRAGMA max_page_count", &maxPages}} {
		if err := s.database.QueryRowContext(ctx, query.sql).Scan(query.dst); err != nil {
			t.Fatal(err)
		}
	}
	if pageSize*maxPages != before.DatabaseLimit || pageCount > maxPages {
		t.Fatalf("capacity not enforced on the replacement connection: pageSize=%d pageCount=%d maxPages=%d limit=%d", pageSize, pageCount, maxPages, before.DatabaseLimit)
	}
}

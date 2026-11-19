package db

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/readtiming"
)

// ReadSnapshot owns one connection and one consistent read transaction. Close
// releases both, including on cancellation. Production display callers supply
// the query-only pool; TxOptions.ReadOnly alone is not SQLite write protection.
type ReadSnapshot struct {
	conn *sql.Conn
	tx   *sql.Tx
	ctx  context.Context
}

func BeginRead(ctx context.Context, pool *sql.DB) (*ReadSnapshot, error) {
	done := readtiming.Start(ctx, "db_acquire")
	conn, err := pool.Conn(ctx)
	done()
	if err != nil {
		return nil, err
	}
	done = readtiming.Start(ctx, "db_begin")
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	done()
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &ReadSnapshot{conn: conn, tx: tx, ctx: ctx}, nil
}

func (r *ReadSnapshot) Close() {
	if r.tx == nil {
		return
	}
	done := readtiming.Start(r.ctx, "db_end")
	r.tx.Rollback()
	r.conn.Close()
	done()
	r.tx = nil
}

type ReadRow struct {
	row  *sql.Row
	done func()
}

func (r *ReadRow) Scan(dest ...any) error { defer r.done(); return r.row.Scan(dest...) }
func (r *ReadSnapshot) QueryRowContext(ctx context.Context, query string, args ...any) *ReadRow {
	done := readtiming.Start(ctx, "sql")
	return &ReadRow{row: r.tx.QueryRowContext(ctx, query, args...), done: done}
}

type ReadRows struct {
	*sql.Rows
	ctx context.Context
}

func (r *ReadSnapshot) QueryContext(ctx context.Context, query string, args ...any) (*ReadRows, error) {
	done := readtiming.Start(ctx, "sql")
	rows, err := r.tx.QueryContext(ctx, query, args...)
	done()
	if err != nil {
		return nil, err
	}
	return &ReadRows{Rows: rows, ctx: ctx}, nil
}
func (r *ReadRows) Next() bool {
	done := readtiming.Start(r.ctx, "sql")
	defer done()
	return r.Rows.Next()
}
func (r *ReadRows) Scan(dest ...any) error {
	done := readtiming.Start(r.ctx, "sql")
	defer done()
	return r.Rows.Scan(dest...)
}
func (r *ReadRows) Close() error {
	done := readtiming.Start(r.ctx, "sql")
	defer done()
	return r.Rows.Close()
}

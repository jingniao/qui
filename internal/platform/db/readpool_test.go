package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TestReadPoolServesQueriesWhileTheWriterIsBusy is the regression test for the
// panel's single-writer design: a long write transaction must not block reads
// that were routed through the read-only pool.
func TestReadPoolServesQueriesWhileTheWriterIsBusy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	writer, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('read-pool','{}')"); err != nil {
		t.Fatal(err)
	}
	readPool, err := OpenReadPool(ctx, path, ReadPoolConnections)
	if err != nil {
		t.Fatal(err)
	}
	defer readPool.Close()
	if _, err := readPool.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('read-pool-write','{}')"); err == nil {
		t.Fatal("read-only pool accepted a write")
	}

	holding := make(chan struct{})
	release := make(chan struct{})
	go func() {
		tx, err := writer.BeginTx(ctx, nil)
		if err != nil {
			t.Error(err)
			close(holding)
			return
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('held','{}')"); err != nil {
			t.Error(err)
			tx.Rollback()
			close(holding)
			return
		}
		close(holding)
		<-release
		tx.Rollback()
	}()
	<-holding
	defer close(release)

	start := time.Now()
	var value string
	if err := readPool.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='read-pool'").Scan(&value); err != nil {
		t.Fatalf("read pool query during a write transaction: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("read pool waited %s for the writer", elapsed)
	}
	if value != "{}" {
		t.Fatalf("unexpected value %q", value)
	}
}

// TestReadPoolDoesNotBlockOnTheSingleWriterConnection proves the pool is a
// separate set of connections, not the writer's one connection.
func TestReadPoolDoesNotBlockOnTheSingleWriterConnection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	writer, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	readPool, err := OpenReadPool(ctx, path, ReadPoolConnections)
	if err != nil {
		t.Fatal(err)
	}
	defer readPool.Close()
	if stats := readPool.Stats(); stats.MaxOpenConnections != ReadPoolConnections {
		t.Fatalf("read pool max connections = %d", stats.MaxOpenConnections)
	}
	blocked, err := writer.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	done := make(chan error, 1)
	go func() {
		var count int
		done <- readPool.QueryRowContext(ctx, "SELECT count(*) FROM system_settings").Scan(&count)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("read pool blocked behind the writer connection")
	}
}

func TestReadPoolRetainsAtMostTwoIdleConnections(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	writer, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, err := OpenReadPool(ctx, path, ReadPoolConnections)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	connections := make([]*sql.Conn, ReadPoolConnections)
	for i := range connections {
		connections[i], err = reader.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer connections[i].Close()
	}
	for _, conn := range connections {
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if stats := reader.Stats(); stats.MaxOpenConnections != ReadPoolConnections || stats.Idle != 2 || stats.OpenConnections != 2 || stats.MaxIdleClosed != 2 {
		t.Fatalf("read pool did not keep two of four reader connections: %+v", stats)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := reader.Stats(); stats.OpenConnections != 0 {
		t.Fatalf("close leaked pooled connections: %+v", stats)
	}
}

func BenchmarkReadPoolNodeCount(b *testing.B) {
	ctx := context.Background()
	path := filepath.Join(b.TempDir(), "panel.db")
	writer, err := Open(ctx, path)
	if err != nil {
		b.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(ctx, `WITH RECURSIVE fixtures(id) AS (
		SELECT 1 UNION ALL SELECT id+1 FROM fixtures WHERE id<13
	) INSERT INTO nodes(id,name,status,created_at)
		SELECT printf('node-%d',id),'fixture node','offline','2026-11-19' FROM fixtures`); err != nil {
		b.Fatal(err)
	}

	for _, name := range []string{"zero_idle", "reuse"} {
		b.Run(name, func(b *testing.B) {
			reader, err := OpenReadPool(ctx, path, ReadPoolConnections)
			if err != nil {
				b.Fatal(err)
			}
			defer reader.Close()
			if name == "zero_idle" {
				reader.SetMaxIdleConns(0)
			}
			startClosed := reader.Stats().MaxIdleClosed
			b.ReportAllocs()
			for b.Loop() {
				read, err := BeginRead(ctx, reader)
				if err != nil {
					b.Fatal(err)
				}
				var count int
				err = read.QueryRowContext(ctx, "SELECT count(*) FROM nodes WHERE deleted_at IS NULL AND (name LIKE ? OR public_ip LIKE ?)", "%fixture%", "%fixture%").Scan(&count)
				read.Close()
				if err != nil || count != 13 {
					b.Fatalf("list count %d: %v", count, err)
				}
			}
			b.ReportMetric(float64(reader.Stats().MaxIdleClosed-startClosed)/float64(b.N), "idle-closes/op")
		})
	}
}

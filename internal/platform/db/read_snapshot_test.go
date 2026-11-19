package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestReadSnapshotIsolationCancellationAndRelease(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	writer, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err = writer.Exec("INSERT INTO system_settings(key,value_json) VALUES('snapshot','1')"); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReadPool(ctx, path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := BeginRead(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	var value string
	if err = tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='snapshot'").Scan(&value); err != nil || value != "1" {
		t.Fatalf("initial value %q: %v", value, err)
	}
	if _, err = writer.Exec("UPDATE system_settings SET value_json='2' WHERE key='snapshot'"); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='snapshot'").Scan(&value); err != nil || value != "1" {
		t.Fatalf("inconsistent snapshot %q: %v", value, err)
	}
	waiting, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err = BeginRead(waiting, reader); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting cancellation: %v", err)
	}
	if _, err = tx.QueryContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('illegal','1') RETURNING key"); err == nil {
		t.Fatal("read pool accepted write")
	}
	tx.Close()
	next, err := BeginRead(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	if err = next.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='snapshot'").Scan(&value); err != nil || value != "2" {
		t.Fatalf("new snapshot %q: %v", value, err)
	}
	next.Close()
	if reader.Stats().OpenConnections != 0 {
		t.Fatal("idle read connection retained")
	}
}

package restoretasks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProgressTransitionsRestartAndTamperRefusal(t *testing.T) {
	ctx := context.Background()
	store := Store{Directory: filepath.Join(t.TempDir(), "records"), Key: bytes.Repeat([]byte{3}, 32)}
	record, err := store.Begin(ctx, "snapshot_fixture_01", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"installing", "committed", "cleaned", "serving"} {
		if _, err := store.Advance(ctx, record.ID, stage, "active", ""); err != nil {
			t.Fatal(stage, err)
		}
	}
	if _, err := store.Advance(ctx, record.ID, "preparing", "active", ""); err == nil {
		t.Fatal("phase regression accepted")
	}
	if err := store.ReconcileInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	records, err := store.List(ctx)
	if err != nil || len(records) != 1 || records[0].Status != "interrupted" || records[0].LastStage != "serving" || records[0].Sequence != 6 {
		t.Fatal("restart fabricated completion", records, err)
	}
	if err := store.ReconcileInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	records, _ = store.List(ctx)
	if records[0].Sequence != 6 {
		t.Fatal("restart reconciliation is not idempotent")
	}
	path := filepath.Join(store.Directory, record.ID+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(original, store.Key) {
		t.Fatal("record exposes authentication key")
	}
	if err := os.WriteFile(path, bytes.Replace(original, []byte("serving"), []byte("stopped"), 1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx); err == nil {
		t.Fatal("modified progress record accepted")
	}
	if err := store.ReconcileInterrupted(ctx); err == nil {
		t.Fatal("tampered record overwritten during recovery")
	}
}

func TestReadOnlyListingMissingStoreAndTerminalStatus(t *testing.T) {
	ctx := context.Background()
	store := Store{Directory: filepath.Join(t.TempDir(), "missing"), Key: bytes.Repeat([]byte{7}, 32)}
	records, err := store.List(ctx)
	if err != nil || len(records) != 0 {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Directory); !os.IsNotExist(err) {
		t.Fatal("read created record directory")
	}
	record, err := store.Begin(ctx, "snapshot_fixture_01", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Advance(ctx, record.ID, "preparing", "failed", "restore_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Advance(ctx, record.ID, "installing", "active", ""); err == nil {
		t.Fatal("terminal record resumed without a new operation")
	}
	if err := store.ReconcileInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	records, _ = store.List(ctx)
	if records[0].Status != "failed" {
		t.Fatal("restart changed terminal status")
	}
}

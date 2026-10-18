package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPreparedConnectionReplacementPreservesPhase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	initial, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	initial.Close()
	prepared, err := OpenPrepared(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	prepared.Database.SetMaxIdleConns(0)
	for i := 0; i < 3; i++ {
		if _, err := prepared.Database.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('before-commit','{}')"); err == nil {
			t.Fatal("reconnected precommit handle allowed writes")
		}
	}
	if err := prepared.EnableWrites(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := prepared.Database.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('after-commit','{}') ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json"); err != nil {
			t.Fatal("reconnected committed handle remained read-only", err)
		}
	}
}

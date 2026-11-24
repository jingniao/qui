package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenNewDatabaseIncrementalAndPreservesExistingModes(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []int{-1, 0, 1, 2} {
		t.Run(string(rune('a'+mode+1)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "panel.db")
			if mode >= 0 {
				database, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				for _, query := range []string{[]string{"PRAGMA auto_vacuum=NONE", "PRAGMA auto_vacuum=FULL", "PRAGMA auto_vacuum=INCREMENTAL"}[mode], "CREATE TABLE existing(value INTEGER)"} {
					if _, err := database.Exec(query); err != nil {
						t.Fatal(err)
					}
				}
				if err := database.Close(); err != nil {
					t.Fatal(err)
				}
			}
			database, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			want := mode
			if mode == -1 {
				want = 2
			}
			var got int
			if err := database.QueryRow("PRAGMA auto_vacuum").Scan(&got); err != nil || got != want {
				t.Fatalf("mode=%d want=%d err=%v", got, want, err)
			}
		})
	}
}

func TestReclaimFreePagesIncludesConnectionWaitInBudget(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	conn, err := database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	started := time.Now()
	_, pending, err := ReclaimFreePages(context.Background(), database)
	if !errors.Is(err, context.DeadlineExceeded) || pending {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("connection wait escaped two-second budget: %s", elapsed)
	}
}

func TestReclaimFreePagesBoundedContinuation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "space.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`PRAGMA auto_vacuum=INCREMENTAL; CREATE TABLE payload(value BLOB); WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<5000) INSERT INTO payload SELECT zeroblob(3500) FROM n; DELETE FROM payload`); err != nil {
		t.Fatal(err)
	}
	free := func() int {
		t.Helper()
		var n int
		if err := database.QueryRow("PRAGMA freelist_count").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := free()
	if before <= 4096 {
		t.Fatalf("insufficient free pages %d", before)
	}
	for pass := 0; pass < 20; pass++ {
		mode, more, err := ReclaimFreePages(ctx, database)
		if err != nil || mode != "incremental" {
			t.Fatalf("mode=%s err=%v", mode, err)
		}
		after := free()
		if before-after <= 0 || before-after > 2048 {
			t.Fatalf("unbounded/no progress before=%d after=%d", before, after)
		}
		if more != (after > 32) {
			t.Fatalf("pending=%v free=%d", more, after)
		}
		if !more {
			return
		}
		before = after
	}
	t.Fatal("free pages never drained")
}

package backup

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func TestArchiveAutoRenewMigrationGate(t *testing.T) {
	ctx := context.Background()
	for _, migrated := range []bool{false, true} {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		db.SetMaxOpenConns(1)
		if _, err := db.Exec("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY)"); err != nil {
			t.Fatal(err)
		}
		if !migrated {
			// A real pre-0160 archive has no auto_renew column.
			if _, err := db.Exec("CREATE TABLE node_meta(renew_date TEXT,renew_cycle TEXT)"); err != nil {
				t.Fatal(err)
			}
			if err := validateNodeAutoRenew(ctx, db); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if _, err := db.Exec("INSERT INTO schema_migrations VALUES('0160_node_meta_auto_renew.sql'); CREATE TABLE node_meta(renew_date TEXT,renew_cycle TEXT,renew_notice_days INTEGER,auto_renew INTEGER)"); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			date, cycle string
			valid       bool
		}{
			{"2025-01-31", "/月", true}, {"2025-01-31", "一次性", false}, {"", "/月", false},
		} {
			if _, err := db.Exec("DELETE FROM node_meta"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("INSERT INTO node_meta VALUES(?,?,7,1)", tc.date, tc.cycle); err != nil {
				t.Fatal(err)
			}
			err := validateNodeAutoRenew(ctx, db)
			if tc.valid && err != nil || !tc.valid && !errors.Is(err, ErrArchive) {
				t.Fatalf("%+v: %v", tc, err)
			}
		}
	}
}

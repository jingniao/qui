package backup

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDeviceLimitBackupRoundTripAndPre0115(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	if _, err := source.Database.Exec("UPDATE users SET device_limit=7"); err != nil {
		t.Fatal(err)
	}
	created, err := source.Create(ctx, directory, "device_limit_backup", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, work string) error {
		db, err := readonlyDatabase(filepath.Join(work, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		if err := validateDeviceLimit(ctx, db); err != nil {
			return err
		}
		var n int
		if err := db.QueryRow("SELECT device_limit FROM users LIMIT 1").Scan(&n); err != nil {
			return err
		}
		if n != 7 {
			t.Fatal("limit not backed up")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"ALTER TABLE users DROP COLUMN device_limit", "DELETE FROM schema_migrations WHERE version='0115_device_limit.sql'"} {
		if _, err := source.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateDeviceLimit(ctx, source.Database); err != nil {
		t.Fatal("older archive refused", err)
	}
}

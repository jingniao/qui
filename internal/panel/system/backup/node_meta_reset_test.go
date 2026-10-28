package backup

import (
	"context"
	"path/filepath"
	"testing"
)

func TestNodeMetaResetBackupValidationAndInventory(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	for _, q := range []string{"INSERT INTO node_meta(node_id,region,renew_date,last_alert_day) SELECT id,'JP','2026-10-31','2026-10-28' FROM nodes", "UPDATE users SET traffic_reset_cycle='monthly',traffic_reset_anchor='2024-01-31T00:00:00.000000000Z',traffic_reset_at='2024-02-29T00:00:00.000000000Z'"} {
		if _, err := source.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	created, err := source.Create(ctx, directory, "node_meta_reset_backup", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, work string) error {
		database, err := readonlyDatabase(filepath.Join(work, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		if err = validateNodeMetaReset(ctx, database); err != nil {
			return err
		}
		inventory, err := ReadInventory(ctx, database)
		if err != nil {
			return err
		}
		if inventory.Totals.NodeMeta < 1 {
			t.Fatal("metadata missing from backup inventory")
		}
		for _, u := range inventory.Users {
			if u.TrafficResetCycle != "monthly" || u.TrafficResetAt != "2024-02-29T00:00:00.000000000Z" {
				t.Fatal(u)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Database.Exec("UPDATE users SET traffic_reset_anchor='invalid'"); err != nil {
		t.Fatal(err)
	}
	if err = validateNodeMetaReset(ctx, source.Database); err == nil {
		t.Fatal("invalid reset anchor accepted")
	}
}

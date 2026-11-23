package backup

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"testing"
	"time"
)

func TestTelemetryRollupsExcludedFromBackup(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	if _, err := source.Database.Exec(`INSERT INTO node_metric_samples(node_id,at,cpu_percent) SELECT id,?,10 FROM nodes LIMIT 1`, time.Now().UTC().Add(-25*time.Hour).Truncate(time.Minute).Format("2006-01-02T15:04:05.000000000Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&node.Repository{Database: source.Database}).CompactTelemetry(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	result, err := source.Create(ctx, directory, "telemetry_backup_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, result.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
		database, e := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
		if e != nil {
			return e
		}
		defer database.Close()
		var count int
		if e = database.QueryRow(`SELECT count(*) FROM node_metric_rollups`).Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			t.Fatalf("monitor rollups leaked into backup: %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var live int
	if err = source.Database.QueryRow(`SELECT count(*) FROM node_metric_rollups`).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 1 {
		t.Fatalf("live rollups changed: %d", live)
	}
}

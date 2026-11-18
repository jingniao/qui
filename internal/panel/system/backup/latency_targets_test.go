package backup

import (
	"context"
	"path/filepath"
	"testing"
)

func TestNodeLatencyTargetsArchive(t *testing.T) {
	t.Parallel()
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	for _, q := range []string{
		"INSERT INTO probe_targets(id,label,host,port) VALUES('custom','custom','1.1.1.1',443)",
		"UPDATE nodes SET latency_targets_custom=1",
		"INSERT INTO node_latency_targets SELECT id,'custom' FROM nodes",
	} {
		if _, err := source.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateNodeLatencyTargets(ctx, source.Database); err != nil {
		t.Fatal(err)
	}
	result, err := source.Create(ctx, directory, "latency_targets_archive", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, result.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
		database, err := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		inventory, err := ReadInventory(ctx, database)
		if err != nil {
			return err
		}
		if inventory.Totals.NodeLatencyTargets == 0 {
			t.Fatal(inventory.Totals)
		}
		var count int
		if err = database.QueryRow("SELECT count(*) FROM nodes WHERE latency_targets_custom=1").Scan(&count); err != nil || count == 0 {
			t.Fatal(count, err)
		}
		return validateNodeLatencyTargets(ctx, database)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Database.Exec("DROP TABLE node_latency_targets"); err != nil {
		t.Fatal(err)
	}
	if validateNodeLatencyTargets(ctx, source.Database) == nil {
		t.Fatal("missing configuration accepted")
	}
}

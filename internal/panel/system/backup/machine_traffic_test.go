package backup

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestMachineTrafficArchiveKeepsDurableRows checks that machine traffic is
// durable traffic data: the archive keeps it while the rate telemetry is
// stripped, and the inventory counts it.
func TestMachineTrafficArchiveKeepsDurableRows(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	day := time.Now().UTC().Format("2006-01-02")
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO node_metric_samples(node_id,at,cpu_percent) SELECT id,?,0 FROM nodes", []any{stamp}},
		{"INSERT INTO node_machine_traffic_settings(node_id,method,interface_name,allowance_bytes,reset_day) SELECT id,'max','eth0',1099511627776,15 FROM nodes", nil},
		{"INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) SELECT id,'eth0',?,100,200 FROM nodes", []any{day}},
		{"INSERT INTO node_machine_traffic_cursors(node_id,interface_name,boot_id,rx_bytes,tx_bytes,observed_at) SELECT id,'eth0','1c0b5a6e-7f6b-4f2f-9c3a-1b2c3d4e5f60',100,200,? FROM nodes", []any{stamp}},
	} {
		if _, err := source.Database.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := ReadInventory(ctx, source.Database)
	if err != nil || inventory.Totals.MachineTrafficSettings != 1 || inventory.Totals.MachineTrafficDays != 1 || inventory.Totals.MachineTrafficCursors != 1 {
		t.Fatalf("inventory: %+v %v", inventory.Totals, err)
	}
	result, err := source.Create(ctx, directory, "machine_traffic_archive", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, result.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
		database, err := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		for _, tc := range []struct {
			query string
			want  int
		}{
			{"SELECT count(*) FROM node_metric_samples", 0},
			{"SELECT count(*) FROM node_machine_traffic_settings WHERE method='max' AND reset_day=15 AND allowance_bytes=1099511627776", 1},
			{"SELECT count(*) FROM node_machine_traffic_daily WHERE rx_bytes=100 AND tx_bytes=200", 1},
			{"SELECT count(*) FROM node_machine_traffic_cursors WHERE boot_id='1c0b5a6e-7f6b-4f2f-9c3a-1b2c3d4e5f60'", 1},
		} {
			var count int
			if err = database.QueryRow(tc.query).Scan(&count); err != nil || count != tc.want {
				t.Fatalf("%s: %d %v", tc.query, count, err)
			}
		}
		archive, err := ReadInventory(ctx, database)
		if err != nil || archive.Totals.MachineTrafficSettings != 1 || archive.Totals.MachineTrafficDays != 1 || archive.Totals.MachineTrafficCursors != 1 {
			t.Fatalf("archive inventory: %+v %v", archive.Totals, err)
		}
		return validateMachineTraffic(ctx, database)
	})
	if err != nil {
		t.Fatal(err)
	}
	// The running panel keeps its rate samples.
	var count int
	if err = source.Database.QueryRow("SELECT count(*) FROM node_metric_samples").Scan(&count); err != nil || count != 1 {
		t.Fatal("live samples stripped", count, err)
	}
}

// TestMachineTrafficValidationRejectsMalformedRows checks the archive checker
// against values the live schema would refuse to store.
func TestMachineTrafficValidationRejectsMalformedRows(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name         string
		statement    string
		ignoreChecks bool
	}{
		{"unknown method", "INSERT INTO node_machine_traffic_settings(node_id,method) SELECT id,'maximum' FROM nodes", true},
		{"loopback interface", "INSERT INTO node_machine_traffic_settings(node_id,interface_name) SELECT id,'lo' FROM nodes", true},
		{"reset day above 31", "INSERT INTO node_machine_traffic_settings(node_id,reset_day) SELECT id,32 FROM nodes", true},
		{"allowance above the exact range", "INSERT INTO node_machine_traffic_settings(node_id,allowance_bytes) SELECT id,9007199254740992 FROM nodes", true},
		{"negative daily amount", "INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) SELECT id,'eth0','2026-01-01',-1,0 FROM nodes", true},
		{"real daily amount", "INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) SELECT id,'eth0','2026-01-01',0.5,0 FROM nodes", true},
		{"non canonical day", "INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) SELECT id,'eth0','2026-1-1',0,0 FROM nodes", false},
		{"invalid day", "INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) SELECT id,'eth0','2026-02-30',0,0 FROM nodes", false},
		{"daily loopback interface", "INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) SELECT id,'lo','2026-01-01',0,0 FROM nodes", false},
		{"non uuid boot id", "INSERT INTO node_machine_traffic_cursors(node_id,interface_name,boot_id,rx_bytes,tx_bytes,observed_at) SELECT id,'eth0','boot',0,0,'2026-01-01T00:00:00.000000000Z' FROM nodes", false},
		{"local cursor timestamp", "INSERT INTO node_machine_traffic_cursors(node_id,interface_name,boot_id,rx_bytes,tx_bytes,observed_at) SELECT id,'eth0','1c0b5a6e-7f6b-4f2f-9c3a-1b2c3d4e5f60',0,0,'2026-01-01T08:00:00+08:00' FROM nodes", false},
	} {
		source, _, _ := sourceFixture(t)
		if tc.ignoreChecks {
			if _, err := source.Database.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := source.Database.Exec(tc.statement); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if validateMachineTraffic(ctx, source.Database) == nil {
			t.Fatalf("%s was accepted", tc.name)
		}
	}
}

// TestMachineTrafficValidationAcceptsPre0126 keeps older archives restorable.
func TestMachineTrafficValidationAcceptsPre0126(t *testing.T) {
	ctx := context.Background()
	source, _, _ := sourceFixture(t)
	for _, statement := range []string{
		"DROP TABLE node_machine_traffic_settings",
		"DROP TABLE node_machine_traffic_daily",
		"DROP TABLE node_machine_traffic_cursors",
		"DELETE FROM schema_migrations WHERE version='0126_node_machine_traffic.sql'",
	} {
		if _, err := source.Database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateMachineTraffic(ctx, source.Database); err != nil {
		t.Fatalf("a pre-0126 archive was rejected: %v", err)
	}
	// Tables without their migration, or a migration without all three tables,
	// are an archive this build did not write.
	if _, err := source.Database.Exec("CREATE TABLE node_machine_traffic_daily(node_id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if validateMachineTraffic(ctx, source.Database) == nil {
		t.Fatal("a partial 0126 schema was accepted")
	}
}

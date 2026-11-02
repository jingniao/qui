package migrations_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"

	_ "modernc.org/sqlite"
)

// preMachineSchema is the embedded migration set without 0126, so the runner
// upgrades a real deployed schema instead of a hand-written fixture.
func preMachineSchema(t *testing.T) fstest.MapFS {
	t.Helper()
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	subset := fstest.MapFS{}
	for _, name := range names {
		if name == "0126_node_machine_traffic.sql" {
			continue
		}
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		subset[name] = &fstest.MapFile{Data: data}
	}
	return subset
}

func TestMachineTrafficMigrationKeepsDeployedData(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if _, err := database.ExecContext(ctx, "PRAGMA foreign_keys(1)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, database, preMachineSchema(t)); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"INSERT INTO nodes(id,name,status,created_at) VALUES('n1','Node 1','online','2026-01-01T00:00:00Z')",
		"INSERT INTO node_meta(node_id,region,remark) VALUES('n1','hk','keep me')",
		"INSERT INTO traffic_daily(bucket,node_id,user_id,device_id,upload_bytes,download_bytes) VALUES('2026-01-01','n1','u1','d1',10,20)",
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatalf("0126 did not apply: %v", err)
	}
	// Deployed data survives the upgrade.
	for _, check := range []struct {
		query string
		want  int
	}{
		{"SELECT count(*) FROM nodes WHERE id='n1'", 1},
		{"SELECT count(*) FROM node_meta WHERE node_id='n1' AND remark='keep me'", 1},
		{"SELECT count(*) FROM traffic_daily WHERE node_id='n1' AND download_bytes=20", 1},
	} {
		var got int
		if err := database.QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s: %d %v", check.query, got, err)
		}
	}
	// A settings row carries the documented defaults.
	if _, err := database.ExecContext(ctx, "INSERT INTO node_machine_traffic_settings(node_id) VALUES('n1')"); err != nil {
		t.Fatal(err)
	}
	var method, iface string
	var allowance sql.NullInt64
	var reset int
	if err := database.QueryRowContext(ctx, "SELECT method,interface_name,allowance_bytes,reset_day FROM node_machine_traffic_settings WHERE node_id='n1'").Scan(&method, &iface, &allowance, &reset); err != nil || method != "sum" || iface != "" || allowance.Valid || reset != 1 {
		t.Fatalf("defaults: %q %q %v %d %v", method, iface, allowance, reset, err)
	}
	// Every constraint the panel relies on is enforced by the schema.
	for _, statement := range []string{
		"INSERT INTO node_machine_traffic_settings(node_id,method) VALUES('n1','maximum')",
		"INSERT INTO node_machine_traffic_settings(node_id,reset_day) VALUES('n1',0)",
		"INSERT INTO node_machine_traffic_settings(node_id,reset_day) VALUES('n1',32)",
		"INSERT INTO node_machine_traffic_settings(node_id,allowance_bytes) VALUES('n1',0)",
		"INSERT INTO node_machine_traffic_settings(node_id,allowance_bytes) VALUES('n1',9007199254740992)",
		"INSERT INTO node_machine_traffic_settings(node_id,allowance_bytes) VALUES('n1',1.5)",
		"INSERT INTO node_machine_traffic_settings(node_id) VALUES('missing')",
		"INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES('n1','eth0','2026-01-01',-1,0)",
		"INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES('n1','eth0','2026-01-01',0.5,0)",
		"INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES('n1','eth0','2026-01-01',1,1),('n1','eth0','2026-01-01',2,2)",
		"INSERT INTO node_machine_traffic_cursors(node_id,interface_name,boot_id,rx_bytes,tx_bytes,observed_at) VALUES('n1','eth0','boot',-1,0,'2026-01-01T00:00:00Z')",
		"INSERT INTO node_machine_traffic_cursors(node_id,interface_name,boot_id,rx_bytes,tx_bytes,observed_at) VALUES('n1','eth0','boot',1,0,'2026-01-01T00:00:00Z'),('n1','eth0','boot',2,0,'2026-01-01T00:00:00Z')",
	} {
		if _, err := database.ExecContext(ctx, statement); err == nil {
			t.Fatalf("accepted: %s", statement)
		}
	}
	// The documented maximum allowance is still accepted.
	if _, err := database.ExecContext(ctx, "UPDATE node_machine_traffic_settings SET allowance_bytes=9007199254740991 WHERE node_id='n1'"); err != nil {
		t.Fatal(err)
	}
	// Running the migrations again changes nothing.
	if err := db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	var days int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM node_machine_traffic_daily").Scan(&days); err != nil || days != 0 {
		t.Fatalf("rerun disturbed the new tables: %d %v", days, err)
	}
}

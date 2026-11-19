package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"proxycontrol/migrations"
)

// legacyDatabase builds what a v1.6.0 panel left behind: the baseline schema
// with the 152 per-file rows in schema_migrations instead of a baseline row.
// done is the value of the one-shot markers a v1.6.0 startup sets to 1.
func legacyDatabase(t *testing.T, keep func(string) bool, done int) *sql.DB {
	t.Helper()
	ctx := context.Background()
	uri, err := URI(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	database.SetMaxOpenConns(1)
	baseline, err := migrations.Files.ReadFile(BaselineMigration)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		"CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TEXT NOT NULL)",
		string(baseline),
	}
	for _, table := range []string{"canvas_tunnel_reconcile", "canvas_forward_reconcile", "import_identity_reconcile"} {
		statements = append(statements, "UPDATE "+table+" SET done="+map[int]string{0: "0", 1: "1"}[done])
	}
	statements = append(statements, "UPDATE relay_cleanup_reconcile SET completed="+map[int]string{0: "0", 1: "1"}[done])
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for name, checksum := range legacyMigrations {
		if keep != nil && !keep(name) {
			continue
		}
		if _, err := database.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(?,?,'2026-11-16T00:00:00Z')", name, checksum); err != nil {
			t.Fatal(err)
		}
	}
	return database
}

func versions(t *testing.T, database *sql.DB) []string {
	t.Helper()
	rows, err := database.Query("SELECT version FROM schema_migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		rows.Scan(&v)
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func TestMigrateAcceptsCompleteLegacyChain(t *testing.T) {
	ctx := context.Background()
	database := legacyDatabase(t, nil, 1)
	if err := Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	got := versions(t, database)
	if len(got) != len(legacyMigrations)+3 || got[len(got)-1] != "0155_traffic_display_hours.sql" {
		t.Fatalf("recorded %d versions, last %v", len(got), got[len(got)-1:])
	}
	for _, v := range got {
		if v == BaselineMigration {
			t.Fatal("baseline recorded for a legacy database")
		}
	}
	var markers int
	if err := database.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('canvas_forward_reconcile','import_identity_map','app_metadata')").Scan(&markers); err != nil || markers != 0 {
		t.Fatalf("one-shot tables left: %d %v", markers, err)
	}
	if err := ValidateMigrationPrefix(ctx, database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal("second run:", err)
	}
}

func TestTrafficDisplayMigrationPreservesExistingSamples(t *testing.T) {
	ctx := context.Background()
	uri, err := URI(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	prior := fstest.MapFS{}
	for _, name := range []string{BaselineMigration, "0153_drop_one_shot_markers.sql", "0154_traffic_storage_retirement.sql"} {
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		prior[name] = &fstest.MapFile{Data: data}
	}
	if err := Migrate(ctx, database, prior); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO nodes(id,name,status,created_at) VALUES('n','n','online','2026-11-19T00:00:00Z');
 INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES('n','xray','e.node','2026-11-19T00:00:01Z',7,13),('n','xray','e.node','2026-11-19T00:00:02Z',11,17);
 INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES('n','e','c','2026-11-19T00:00:03Z','u','d',5,9);`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	var buckets, ready, up, down int64
	if err := database.QueryRow(`SELECT count(*),sum(ready) FROM traffic_sample_hours`).Scan(&buckets, &ready); err != nil || buckets != 2 || ready != 0 {
		t.Fatalf("unprepared index: %d/%d %v", buckets, ready, err)
	}
	if err := database.QueryRow(`SELECT sum(upload_bytes),sum(download_bytes) FROM core_traffic_samples`).Scan(&up, &down); err != nil || up != 18 || down != 30 {
		t.Fatalf("source changed: %d/%d %v", up, down, err)
	}
	// Updates before backfill stay unprepared rather than publishing partial sums.
	if _, err := database.Exec(`UPDATE core_traffic_samples SET upload_bytes=upload_bytes+1`); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT sum(ready),sum(upload_bytes) FROM traffic_sample_hours`).Scan(&ready, &up); err != nil || ready != 0 || up != 0 {
		t.Fatalf("partial index exposed: %d/%d %v", ready, up, err)
	}
	if err := Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
}

func TestMigrateRefusesPartialLegacyChain(t *testing.T) {
	ctx := context.Background()
	database := legacyDatabase(t, func(name string) bool { return name < "0150" }, 1)
	if err := Migrate(ctx, database, migrations.Files); !errors.Is(err, ErrPredatesBaseline) {
		t.Fatalf("migrate: %v", err)
	}
	if got := versions(t, database); len(got) != 149 {
		t.Fatalf("refused migration still wrote: %d rows", len(got))
	}
	if err := ValidateMigrationPrefix(ctx, database, migrations.Files); !errors.Is(err, ErrPredatesBaseline) {
		t.Fatalf("validate: %v", err)
	}
	if verdict := inspect(ctx, database, migrations.Files).Verdict; verdict != VerdictOutdated {
		t.Fatalf("verdict %s", verdict)
	}
}

func TestMigrateRefusesUnfinishedOneShotConversion(t *testing.T) {
	database := legacyDatabase(t, nil, 0)
	err := Migrate(context.Background(), database, migrations.Files)
	if err == nil || !strings.Contains(err.Error(), "start_v1_6_0_once_before_upgrading") {
		t.Fatalf("migrate: %v", err)
	}
	var markers int
	if err := database.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='canvas_forward_reconcile'").Scan(&markers); err != nil || markers != 1 {
		t.Fatalf("failed migration dropped tables: %d %v", markers, err)
	}
}

func TestMigrateRefusesChangedLegacyChecksum(t *testing.T) {
	database := legacyDatabase(t, nil, 1)
	if _, err := database.Exec("UPDATE schema_migrations SET checksum='x' WHERE version='0042_node_restart_requests.sql'"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(context.Background(), database, migrations.Files); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("migrate: %v", err)
	}
}

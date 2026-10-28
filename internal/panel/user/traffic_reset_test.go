package user

import (
	"context"
	"database/sql"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"
	"testing"
	"time"
)

func TestTrafficResetRemarkMigrationFixtures(t *testing.T) {
	ctx := context.Background()
	uri, err := db.URI(filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if err = db.Migrate(ctx, database, beforeMigration{FS: migrations.Files, stop: "0118_traffic_reset.sql"}); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct{ remark, cycle, want string }{
		{"  原样\r\n备注\t ", "none", "  原样\r\n备注\t "},
		{"[config:cycle=monthly;up=10;down=20] \n保留 末尾 ", "monthly", "保留 末尾 "},
		{"[config:up=2;cycle=30days;down=3]备注", "30days", "备注"},
		{"[config:cycle=invalid;up=0] \t", "none", ""},
		{"[config:cycle=monthly;cycle=30days] x", "30days", "x"},
		{"x[config:cycle=monthly] x", "none", "x[config:cycle=monthly] x"},
		{"[config:cycle=monthly", "none", "[config:cycle=monthly"},
	}
	for i, f := range fixtures {
		if _, err = database.Exec(`INSERT INTO users(id,username,display_name,status,max_devices,max_connections,protocols_json,remark,created_at) VALUES(?,?,?,'active',1,1,'["socks5"]',?,'2024-01-31T00:00:00.000000000Z')`, i, i, i, f.remark); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	for i, f := range fixtures {
		var remark, cycle string
		if err = database.QueryRow("SELECT remark,traffic_reset_cycle FROM users WHERE id=?", i).Scan(&remark, &cycle); err != nil {
			t.Fatal(err)
		}
		if remark != f.want || cycle != f.cycle {
			t.Fatalf("fixture %d: %q %s", i, remark, cycle)
		}
	}
	if err = db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal("repeat migration", err)
	}
}

func TestTrafficResetCalendarFakeClock(t *testing.T) {
	for _, test := range []struct{ anchor, now, cycle, want, zone string }{
		{"2023-01-31", "2023-02-28", "monthly", "2023-02-28", "UTC"},
		{"2024-01-31", "2024-02-29", "monthly", "2024-02-29", "UTC"},
		{"2024-01-31", "2024-03-30", "monthly", "2024-02-29", "UTC"},
		{"2024-01-31", "2024-03-31", "monthly", "2024-03-31", "UTC"},
		{"2024-01-31", "2024-03-01", "30days", "2024-03-01", "UTC"},
		{"2024-03-01", "2024-03-31", "30days", "2024-03-31", "America/New_York"},
	} {
		t.Run(test.now+test.cycle+test.zone, func(t *testing.T) {
			loc, e := time.LoadLocation(test.zone)
			if e != nil {
				t.Fatal(e)
			}
			a, _ := time.ParseInLocation("2006-01-02", test.anchor, loc)
			now, _ := time.ParseInLocation("2006-01-02", test.now, loc)
			if got := resetBoundary(a, now, test.cycle).Format("2006-01-02"); got != test.want {
				t.Fatal(got, test.want)
			}
		})
	}
}

func TestTrafficResetDueTimezoneIdempotentAndDisabled(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	input := userInput()
	input.TrafficResetCycle = "monthly"
	input.TrafficResetAnchor = "2024-01-31T16:00:00Z"
	input.Status = "disabled"
	u, err := r.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Database.Exec("UPDATE users SET used_traffic=100 WHERE id=?", u.ID); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2024, 2, 29, 16, 0, 0, 0, time.UTC)
	ids, err := r.ResetDue(ctx, now.Add(-time.Nanosecond), loc)
	if err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	ids, err = r.ResetDue(ctx, now, loc)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	got, err := r.Get(ctx, u.ID)
	if err != nil || got.UsedTraffic != 0 || got.Status != "disabled" || got.TrafficResetAt != timestamp(now) {
		t.Fatal(got, err)
	}
	if _, err = r.Database.Exec("UPDATE users SET used_traffic=7 WHERE id=?", u.ID); err != nil {
		t.Fatal(err)
	}
	// A new Repository represents a panel restart; the claim is persisted.
	restarted := &Repository{Database: r.Database, Vault: r.Vault}
	ids, err = restarted.ResetDue(ctx, now, loc)
	if err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	got, _ = r.Get(ctx, u.ID)
	if got.UsedTraffic != 7 {
		t.Fatal("duplicate reset")
	}
}

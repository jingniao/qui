package system

import (
	"context"
	"errors"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"sync"
	"testing"
)

func TestBasePersistenceCanonicalSetupAndAuditAtomicity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	database, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { database.Close() }()
	r := &Repository{Database: database}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SaveSetup(ctx, tx, "  Internal Panel  ", " UTC "); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	initial, err := r.ReadBase(ctx)
	if err != nil || initial.PlatformName != "Internal Panel" || initial.Timezone != "UTC" || initial.Revision != 0 {
		t.Fatal(initial, err)
	}
	input := BaseInput{PlatformName: "代理控制台", Timezone: "America/New_York"}
	saved, changed, err := r.SaveBase(ctx, input, "owner", "ip")
	if err != nil || !changed || saved.Revision != 1 {
		t.Fatal(saved, changed, err)
	}
	name, zone, err := r.ReadSetup(ctx)
	if err != nil || name != input.PlatformName || zone != input.Timezone {
		t.Fatal("session settings did not read current persisted base", name, zone, err)
	}
	if _, changed, err := r.SaveBase(ctx, input, "owner", "ip"); err != nil || changed {
		t.Fatal("retry duplicated version or event", changed, err)
	}
	var audits int
	database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='修改平台基础设置'").Scan(&audits)
	if audits != 1 {
		t.Fatal("duplicate audit", audits)
	}
	input.PlatformName = "Other"
	if _, _, err := r.SaveBase(ctx, input, "owner", "ip"); !errors.Is(err, ErrBaseConflict) {
		t.Fatal("stale view overwrote base", err)
	}
	input.ExpectedRevision = 1
	if _, err := database.Exec("CREATE TRIGGER fail_base_audit BEFORE INSERT ON admin_operations BEGIN SELECT RAISE(ABORT,'audit unavailable');END"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.SaveBase(ctx, input, "owner", "ip"); err == nil {
		t.Fatal("audit failure accepted")
	}
	current, err := r.ReadBase(ctx)
	if err != nil || current != saved {
		t.Fatal("name, timezone and version partially committed", current, err)
	}
	database.Close()
	database, err = db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	r.Database = database
	current, err = r.ReadBase(ctx)
	if err != nil || current != saved {
		t.Fatal("restart changed base", current, err)
	}
}
func TestBaseConcurrentViewsAndInvalidExplicitTimezone(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, zone := range []string{"UTC", "Asia/Tokyo"} {
		wait.Add(1)
		go func(zone string) {
			defer wait.Done()
			<-start
			_, _, err := r.SaveBase(ctx, BaseInput{PlatformName: "Concurrent Panel", Timezone: zone}, "owner", "ip")
			results <- err
		}(zone)
	}
	close(start)
	wait.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrBaseConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("two stale views both updated", success)
	}
	for _, input := range []BaseInput{{PlatformName: " ", Timezone: "UTC"}, {PlatformName: "bad\nname", Timezone: "UTC"}, {PlatformName: "Panel", Timezone: ""}, {PlatformName: "Panel", Timezone: "Local"}, {PlatformName: "Panel", Timezone: "Not/AZone"}} {
		if _, _, err := r.SaveBase(ctx, input, "owner", "ip"); !errors.Is(err, ErrBaseInvalid) {
			t.Fatal("invalid name or implicit machine timezone accepted", err)
		}
	}
	if _, err := database.Exec("UPDATE system_settings SET value_json='\"Invalid/Zone\"' WHERE key='timezone'"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.ReadSetup(ctx); !errors.Is(err, ErrBaseInvalid) {
		t.Fatal("invalid persisted timezone silently repaired", err)
	}
}

package system

import (
	"context"
	"errors"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	contract "proxycontrol/pkg/contracts/access"
	"sync"
	"testing"
)

func TestRetentionDefaultsPersistenceConflictAndAuditAtomicity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	database, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { database.Close() }()
	r := &Repository{Database: database}
	initial, err := r.ReadRetention(ctx)
	if err != nil || initial.Policy != contract.DefaultRetention || initial.Revision != 0 {
		t.Fatal(initial, err)
	}
	policy := contract.Retention{AllowedDays: 7, BlockedDays: 30, HourlyDays: 14}
	saved, err := r.SaveRetention(ctx, RetentionInput{Policy: policy}, "owner", "192.0.2.1")
	if err != nil || saved.Revision != 1 || saved.UpdatedBy != "owner" {
		t.Fatal(saved, err)
	}
	if repeat, err := r.SaveRetention(ctx, RetentionInput{Policy: policy}, "owner", "192.0.2.1"); err != nil || repeat.Revision != 1 {
		t.Fatal("retry duplicated settings version", repeat, err)
	}
	var operations int
	database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='修改访问记录保留策略'").Scan(&operations)
	if operations != 1 {
		t.Fatal("retry duplicated audit", operations)
	}
	if _, err := r.SaveRetention(ctx, RetentionInput{Policy: contract.Retention{AllowedDays: 8, BlockedDays: 30, HourlyDays: 14}}, "owner", "ip"); !errors.Is(err, ErrRetentionConflict) {
		t.Fatal("stale write overwrote policy", err)
	}
	if _, err := database.Exec("CREATE TRIGGER fail_retention_audit BEFORE INSERT ON admin_operations BEGIN SELECT RAISE(ABORT,'audit unavailable');END"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SaveRetention(ctx, RetentionInput{Policy: contract.Retention{AllowedDays: 8, BlockedDays: 30, HourlyDays: 14}, ExpectedRevision: 1}, "owner", "ip"); err == nil {
		t.Fatal("audit failure committed policy")
	}
	current, err := r.ReadRetention(ctx)
	if err != nil || current.Policy != policy || current.Revision != 1 {
		t.Fatal("failed audit left partial settings", current, err)
	}
	if _, err := database.Exec("DROP TRIGGER fail_retention_audit"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	database, err = db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	r.Database = database
	current, err = r.ReadRetention(ctx)
	if err != nil || current != saved {
		t.Fatal("restart lost policy", current, err)
	}
}
func TestConcurrentRetentionWritersAndInvalidStoredPolicy(t *testing.T) {
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
	for _, days := range []int{7, 8} {
		wait.Add(1)
		go func(days int) {
			defer wait.Done()
			<-start
			_, err := r.SaveRetention(ctx, RetentionInput{Policy: contract.Retention{AllowedDays: days, BlockedDays: 30, HourlyDays: 14}}, "owner", "ip")
			results <- err
		}(days)
	}
	close(start)
	wait.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrRetentionConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("concurrent views both overwrote initial revision", success)
	}
	for _, policy := range []contract.Retention{{AllowedDays: 0, BlockedDays: 1, HourlyDays: 1}, {AllowedDays: 1, BlockedDays: 3651, HourlyDays: 1}, {AllowedDays: 1, BlockedDays: 1, HourlyDays: -1}} {
		if _, err := r.SaveRetention(ctx, RetentionInput{Policy: policy, ExpectedRevision: 1}, "owner", "ip"); !errors.Is(err, ErrRetentionInvalid) {
			t.Fatal("invalid days persisted", err)
		}
	}
	if _, err := database.Exec("UPDATE system_settings SET value_json=? WHERE key='access_retention'", `{"policy":{"allowedDays":1,"blockedDays":1,"hourlyDays":1},"revision":1,"updatedAt":"bad-date","updatedBy":"owner"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadRetention(ctx); !errors.Is(err, ErrRetentionInvalid) {
		t.Fatal("corrupt persisted settings silently used defaults", err)
	}
}

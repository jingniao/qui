package system

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAutomaticRetentionKeepsSevenDailyFourWeeklyThreeMonthlyAndManualFiles(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	enableBackupSchedule(t, s)
	manual, err := s.Create(ctx, "owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 5, 1, 20, 0, 0, 0, time.UTC)
	for day := 0; day < 25; day++ {
		if err := s.RunDue(ctx, start.AddDate(0, 0, day)); err != nil {
			t.Fatal("automatic daily/week rotation failed", day, err)
		}
	}
	for _, month := range []time.Month{time.July, time.August, time.September, time.October} {
		if err := s.RunDue(ctx, time.Date(2026, month, 1, 20, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal("monthly rotation failed", month, err)
		}
	}
	for kind, want := range map[string]int{"daily": 7, "weekly": 4, "monthly": 3, "manual": 1} {
		var count int
		if err := s.Repository.Database.QueryRow("SELECT count(*) FROM backups WHERE kind=?", kind).Scan(&count); err != nil || count != want {
			t.Fatal("wrong retained count", kind, count, want, err)
		}
	}
	files, err := filepath.Glob(filepath.Join(s.Directory, "*.pcb"))
	if err != nil || len(files) != 15 {
		t.Fatal("physical file rotation disagrees with catalog", len(files), err)
	}
	if _, err := os.Stat(filepath.Join(s.Directory, manual.ID+".pcb")); err != nil {
		t.Fatal("automatic rotation removed manual backup", err)
	}
	var completed, retired int
	s.Repository.Database.QueryRow("SELECT count(*) FROM backup_runs WHERE status='success'").Scan(&completed)
	s.Repository.Database.QueryRow("SELECT count(*) FROM backup_runs WHERE status='success' AND backup_id IS NULL").Scan(&retired)
	if completed <= 14 || retired != completed-14 {
		t.Fatal("retired period receipts lost or inconsistent", completed, retired)
	}
	if err := s.RunDue(ctx, time.Date(2026, time.October, 1, 20, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, total, err := s.Repository.Backups(ctx, 1, 100); err != nil || total != 15 {
		t.Fatal("completed retained period was recreated", total, err)
	}
}

func TestFailedBackupOrUnusableNewestFileCannotPruneExistingArchives(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	policy := defaultBackupPolicy
	policy.Enabled = true
	policy.DailyKeep = 1
	settings, _, err := s.SaveSchedule(ctx, BackupScheduleInput{Policy: policy, Password: backupTestPassword}, "owner", "ip")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	if err := s.RunDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	old, _, err := s.Repository.Backups(ctx, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repository.Database.Exec("CREATE TRIGGER fail_rotation_audit BEFORE INSERT ON admin_operations WHEN NEW.action='创建系统备份' BEGIN SELECT RAISE(ABORT,'audit unavailable');END"); err != nil {
		t.Fatal(err)
	}
	if err := s.RunDue(ctx, now.AddDate(0, 0, 1)); err == nil {
		t.Fatal("failed automatic creation appeared successful")
	}
	for _, item := range old {
		if _, err := s.Repository.Backup(ctx, item.ID); err != nil {
			t.Fatal("failed new backup pruned old catalog", err)
		}
		if _, err := os.Stat(filepath.Join(s.Directory, item.ID+".pcb")); err != nil {
			t.Fatal("failed new backup pruned old file", err)
		}
	}
	if _, err := s.Repository.Database.Exec("DROP TRIGGER fail_rotation_audit"); err != nil {
		t.Fatal(err)
	}
	policy.DailyKeep = 2
	settings, _, err = s.SaveSchedule(ctx, BackupScheduleInput{Policy: policy, ExpectedRevision: settings.Revision}, "owner", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunDue(ctx, now.AddDate(0, 0, 1).Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Repository.Database.Query("SELECT id FROM backups WHERE kind='daily' ORDER BY created_at DESC")
	if err != nil {
		t.Fatal(err)
	}
	daily := []string{}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		daily = append(daily, id)
	}
	rows.Close()
	if len(daily) != 2 {
		t.Fatal("fixture did not preserve both daily archives", len(daily))
	}
	path := filepath.Join(s.Directory, daily[0]+".pcb")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), data...)
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	policy.DailyKeep = 1
	if _, _, err := s.SaveSchedule(ctx, BackupScheduleInput{Policy: policy, ExpectedRevision: settings.Revision}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	if err := s.RunDue(ctx, now.AddDate(0, 0, 1).Add(2*time.Minute)); err == nil {
		t.Fatal("corrupted newest anchor allowed rotation")
	}
	var remaining int
	s.Repository.Database.QueryRow("SELECT count(*) FROM backups WHERE kind='daily'").Scan(&remaining)
	if remaining != 2 {
		t.Fatal("rotation removed last good daily archive", remaining)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RunDue(ctx, now.AddDate(0, 0, 1).Add(3*time.Minute)); err != nil {
		t.Fatal("repaired retained archive did not permit retry", err)
	}
	remainingFiles, err := filepath.Glob(filepath.Join(s.Directory, "*.pcb"))
	if err != nil || len(remainingFiles) != 4 {
		t.Fatal("repaired cleanup retained wrong physical count", len(remainingFiles), err)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("rotation changed protected newest artifact", err)
	}
}

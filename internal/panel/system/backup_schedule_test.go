package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"testing"
	"time"
)

func enableBackupSchedule(t *testing.T, s *BackupService) BackupSchedule {
	t.Helper()
	policy := defaultBackupPolicy
	policy.Enabled = true
	result, changed, err := s.SaveSchedule(context.Background(), BackupScheduleInput{Policy: policy, Password: backupTestPassword}, "owner", "ip")
	if err != nil || !changed {
		t.Fatal(result, changed, err)
	}
	return result
}

func TestBackupScheduleDefaultsEncryptedPersistenceConflictAndAuditRollback(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	initial, err := s.Repository.BackupSchedule(ctx)
	if err != nil || initial.Policy != defaultBackupPolicy || initial.Revision != 0 || initial.PasswordConfigured {
		t.Fatal(initial, err)
	}
	policy := defaultBackupPolicy
	policy.Enabled = true
	if _, _, err := s.SaveSchedule(ctx, BackupScheduleInput{Policy: policy}, "owner", "ip"); !errors.Is(err, ErrBackupScheduleInvalid) {
		t.Fatal("automatic encryption admitted without password", err)
	}
	saved := enableBackupSchedule(t, s)
	var stored string
	if err := s.Repository.Database.QueryRow("SELECT value_json FROM system_settings WHERE key='backup_schedule'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(saved)
	if bytes.Contains([]byte(stored), []byte(backupTestPassword)) || bytes.Contains(encoded, []byte("encrypted_password")) || bytes.Contains(encoded, []byte(backupTestPassword)) {
		t.Fatal("automatic backup password exposed")
	}
	if duplicate, changed, err := s.SaveSchedule(ctx, BackupScheduleInput{Policy: policy, Password: backupTestPassword}, "owner", "ip"); err != nil || changed || duplicate.Revision != saved.Revision {
		t.Fatal("retry duplicated schedule version", duplicate, changed, err)
	}
	policy.Time = "04:00"
	if _, _, err := s.SaveSchedule(ctx, BackupScheduleInput{Policy: policy}, "owner", "ip"); !errors.Is(err, ErrBackupScheduleConflict) {
		t.Fatal("stale schedule overwrote current policy", err)
	}
	if _, err := s.Repository.Database.Exec("CREATE TRIGGER fail_schedule_audit BEFORE INSERT ON admin_operations BEGIN SELECT RAISE(ABORT,'audit unavailable');END"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SaveSchedule(ctx, BackupScheduleInput{Policy: policy, ExpectedRevision: saved.Revision}, "owner", "ip"); err == nil {
		t.Fatal("settings saved without audit")
	}
	current, err := s.Repository.BackupSchedule(ctx)
	if err != nil || current != saved {
		t.Fatal("audit failure partially saved policy", current, err)
	}
	if _, err := s.Repository.Database.Exec("DROP TRIGGER fail_schedule_audit"); err != nil {
		t.Fatal(err)
	}
	path := s.Source.Config.Database
	s.Repository.Database.Close()
	database, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s.Repository.Database = database
	s.Source.Database = database
	current, err = s.Repository.BackupSchedule(ctx)
	if err != nil || current != saved {
		t.Fatal("schedule lost on restart", current, err)
	}
}

func TestAutomaticPeriodsRunOnceAcrossRestartAndDST(t *testing.T) {
	s := backupFixture(t)
	enableBackupSchedule(t, s)
	ctx := context.Background()
	now := time.Date(2026, 10, 3, 19, 0, 0, 0, time.UTC) // Asia/Shanghai: Oct 4 at 03:00.
	if err := s.RunDue(ctx, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, total, err := s.Repository.Backups(ctx, 1, 100); err != nil || total != 0 {
		t.Fatal("automatic backup ran before scheduled time", total, err)
	}
	if err := s.RunDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, total, err := s.Repository.Backups(ctx, 1, 100); err != nil || total != 3 {
		t.Fatal("due day/week/month not backed up", total, err)
	}
	restarted := &BackupService{Repository: s.Repository, Source: s.Source, Directory: s.Directory}
	if err := restarted.RunDue(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, total, err := s.Repository.Backups(ctx, 1, 100); err != nil || total != 3 {
		t.Fatal("restart duplicated completed periods", total, err)
	}
	if err := restarted.RunDue(ctx, now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if _, total, err := s.Repository.Backups(ctx, 1, 100); err != nil || total != 5 {
		t.Fatal("new local day and ISO week not distinguished", total, err)
	}
	dst := backupFixture(t)
	if _, _, err := dst.Repository.SaveBase(ctx, BaseInput{PlatformName: "DST", Timezone: "America/New_York"}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	policy := defaultBackupPolicy
	policy.Enabled = true
	policy.Time = "02:30"
	if _, _, err := dst.SaveSchedule(ctx, BackupScheduleInput{Policy: policy, Password: backupTestPassword}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	// The missing local 02:30 spring-forward slot is caught up at 03:00.
	if err := dst.RunDue(ctx, time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, total, err := dst.Repository.Backups(ctx, 1, 100); err != nil || total != 3 {
		t.Fatal("DST gap skipped automatic backup", total, err)
	}
	settings, err := dst.Repository.BackupSchedule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	policy.Time = "01:30"
	if _, _, err := dst.SaveSchedule(ctx, BackupScheduleInput{Policy: policy, ExpectedRevision: settings.Revision}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	if err := dst.RunDue(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := dst.RunDue(ctx, first.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, total, err := dst.Repository.Backups(ctx, 1, 100); err != nil || total != 6 {
		t.Fatal("repeated DST hour duplicated a period", total, err)
	}
}

func TestClaimLeaseFailureRetryAndStaleCompletionCannotPublish(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	enableBackupSchedule(t, s)
	now := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	first, err := s.claimBackup(ctx, now, "daily")
	if err != nil || first.claim == nil {
		t.Fatal(err)
	}
	other := &BackupService{Repository: s.Repository, Source: s.Source, Directory: s.Directory}
	if pending, err := other.claimBackup(ctx, now.Add(time.Minute), "daily"); err != nil || pending.claim != nil {
		t.Fatal("active lease was taken twice", pending.claim, err)
	}
	second, err := other.claimBackup(ctx, now.Add(6*time.Minute), "daily")
	if err != nil || second.claim == nil || second.claim.Token == first.claim.Token {
		t.Fatal("interrupted lease could not retry", err)
	}
	if _, err := s.createLocked(ctx, "system", "", first.password, "daily", first.claim); !errors.Is(err, ErrBackupScheduleConflict) {
		t.Fatal("stale worker published catalog", err)
	}
	if _, total, err := s.Repository.Backups(ctx, 1, 100); err != nil || total != 0 {
		t.Fatal("stale worker left a published backup", total, err)
	}
	if err := other.failedBackup(ctx, second.claim, now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.claimBackup(ctx, now.Add(7*time.Minute), "daily"); err != nil || pending.claim != nil {
		t.Fatal("retry backoff ignored", pending.claim, err)
	}
	third, err := s.claimBackup(ctx, now.Add(9*time.Minute), "daily")
	if err != nil || third.claim == nil {
		t.Fatal("failed period never retried", err)
	}
	if _, err := s.createLocked(ctx, "system", "", third.password, "daily", third.claim); err != nil {
		t.Fatal(err)
	}
	runs, total, err := s.Repository.BackupRuns(ctx, 1, 20)
	if err != nil || total != 1 || runs[0].Status != "success" || runs[0].Attempts != 3 {
		t.Fatal("durable retries or result binding incorrect", runs, total, err)
	}
}

func TestChangedOrDisabledPolicyBlocksOldWorkerAndPreservesExistingBackups(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	settings := enableBackupSchedule(t, s)
	manual, err := s.Create(ctx, "owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	due, err := s.claimBackup(ctx, time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC), "daily")
	if err != nil || due.claim == nil {
		t.Fatal(err)
	}
	policy := settings.Policy
	policy.Enabled = false
	if _, _, err := s.SaveSchedule(ctx, BackupScheduleInput{Policy: policy, ExpectedRevision: settings.Revision}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.createLocked(ctx, "system", "", due.password, "daily", due.claim); !errors.Is(err, ErrBackupScheduleConflict) {
		t.Fatal("disabled policy allowed stale worker success", err)
	}
	if _, err := s.Repository.Backup(ctx, manual.ID); err != nil {
		t.Fatal("disabled job removed existing manual archive", err)
	}
	entries, err := filepath.Glob(filepath.Join(s.Directory, "*.pcb"))
	if err != nil || len(entries) != 1 {
		t.Fatal("failed worker left extra archive", entries, err)
	}
}

func TestInvalidStoredScheduleAndPasswordDoNotCreateOrCleanArchives(t *testing.T) {
	for _, fault := range []string{"unknown-field", "invalid-time", "missing-password", "corrupt-password"} {
		t.Run(fault, func(t *testing.T) {
			s := backupFixture(t)
			enableBackupSchedule(t, s)
			ctx := context.Background()
			manual, err := s.Create(ctx, "owner", "ip", backupTestPassword)
			if err != nil {
				t.Fatal(err)
			}
			var data string
			if err := s.Repository.Database.QueryRow("SELECT value_json FROM system_settings WHERE key='backup_schedule'").Scan(&data); err != nil {
				t.Fatal(err)
			}
			var stored map[string]any
			if err := json.Unmarshal([]byte(data), &stored); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "unknown-field":
				stored["command"] = "shell"
			case "invalid-time":
				stored["policy"].(map[string]any)["time"] = "99:99"
			case "missing-password":
				stored["encrypted_password"] = ""
			case "corrupt-password":
				stored["encrypted_password"] = "YnJva2Vu"
			}
			encoded, _ := json.Marshal(stored)
			if _, err := s.Repository.Database.Exec("UPDATE system_settings SET value_json=? WHERE key='backup_schedule'", string(encoded)); err != nil {
				t.Fatal(err)
			}
			if err := s.RunDue(ctx, time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)); err == nil {
				t.Fatal("invalid persisted automation ran")
			}
			if _, total, err := s.Repository.Backups(ctx, 1, 20); err != nil || total != 1 {
				t.Fatal("invalid automation altered catalog", total, err)
			}
			if _, err := s.Repository.Backup(ctx, manual.ID); err != nil {
				t.Fatal("invalid automation deleted manual backup", err)
			}
			if files, err := filepath.Glob(filepath.Join(s.Directory, "*.pcb")); err != nil || len(files) != 1 {
				t.Fatal("invalid automation altered stored files", len(files), err)
			}
		})
	}
}

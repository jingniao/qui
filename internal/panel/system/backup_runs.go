package system

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"proxycontrol/internal/platform/id"
	"strconv"
	"time"
)

type BackupRun struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	PeriodKey        string `json:"periodKey"`
	ScheduleRevision int64  `json:"scheduleRevision"`
	Status           string `json:"status"`
	Attempts         int64  `json:"attempts"`
	BackupID         string `json:"backupId"`
	LastAttemptAt    string `json:"lastAttemptAt"`
	RetryAt          string `json:"retryAt"`
	ErrorCode        string `json:"errorCode"`
}
type backupClaim struct {
	ID, Token        string
	ScheduleRevision int64
	Kind, PeriodKey  string
	Attempts         int64
}
type dueBackup struct {
	claim    *backupClaim
	stored   storedBackupSchedule
	password string
	complete bool
}

func backupPeriod(kind string, local time.Time) string {
	switch kind {
	case "daily":
		return local.Format("2006-01-02")
	case "weekly":
		year, week := local.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case "monthly":
		return local.Format("2006-01")
	}
	return ""
}

func (s *BackupService) claimBackup(ctx context.Context, now time.Time, kind string) (dueBackup, error) {
	var due dueBackup
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return due, err
	}
	defer tx.Rollback()
	stored, err := readBackupSchedule(ctx, tx)
	if err != nil {
		return due, err
	}
	due.stored = stored
	if !stored.Policy.Enabled {
		return due, nil
	}
	base, err := readBase(ctx, tx)
	if err != nil {
		return due, err
	}
	location, err := time.LoadLocation(base.Timezone)
	if err != nil {
		return due, ErrBackupScheduleInvalid
	}
	local := now.In(location)
	hour, _ := strconv.Atoi(stored.Policy.Time[:2])
	minute, _ := strconv.Atoi(stored.Policy.Time[3:])
	if local.Hour()*60+local.Minute() < hour*60+minute {
		return due, nil
	}
	period := backupPeriod(kind, local)
	if period == "" || local.Year() < 2000 || local.Year() > 9999 {
		return due, ErrBackupScheduleInvalid
	}
	password, err := backupPassword(s.Source.ConfigurationVault, stored)
	if err != nil {
		return due, err
	}
	var runID, status, lease, retry string
	var attempts, oldRevision int64
	err = tx.QueryRowContext(ctx, "SELECT id,status,attempts,lease_until,retry_at,schedule_revision FROM backup_runs WHERE kind=? AND period_key=?", kind, period).Scan(&runID, &status, &attempts, &lease, &retry, &oldRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return due, err
	}
	if status == "success" {
		due.complete = true
		return due, nil
	}
	if status == "running" {
		until, err := time.Parse(time.RFC3339Nano, lease)
		if err != nil {
			return due, ErrBackupScheduleInvalid
		}
		if until.After(now) {
			return due, nil
		}
	}
	if status == "failed" && oldRevision == stored.Revision && retry != "" {
		until, err := time.Parse(time.RFC3339Nano, retry)
		if err != nil {
			return due, ErrBackupScheduleInvalid
		}
		if until.After(now) {
			return due, nil
		}
	}
	if attempts == math.MaxInt64 {
		return due, ErrBackupScheduleInvalid
	}
	if runID == "" {
		runID, err = id.New()
		if err != nil {
			return due, err
		}
	}
	token, err := id.New()
	if err != nil {
		return due, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	until := now.Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "INSERT INTO backup_runs(id,kind,period_key,schedule_revision,status,attempts,claim_token,lease_until,last_attempt_at) VALUES(?,?,?,?,'running',?,?,?,?) ON CONFLICT(kind,period_key) DO UPDATE SET schedule_revision=excluded.schedule_revision,status='running',attempts=excluded.attempts,claim_token=excluded.claim_token,lease_until=excluded.lease_until,last_attempt_at=excluded.last_attempt_at,retry_at='',error_code=''", runID, kind, period, stored.Revision, attempts+1, token, until, stamp); err != nil {
		return due, err
	}
	if err := tx.Commit(); err != nil {
		return due, err
	}
	due.claim = &backupClaim{ID: runID, Token: token, ScheduleRevision: stored.Revision, Kind: kind, PeriodKey: period, Attempts: attempts + 1}
	due.password = password
	if s.Notify != nil {
		s.Notify()
	}
	return due, nil
}

func (s *BackupService) failedBackup(ctx context.Context, claim *backupClaim, now time.Time) error {
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	delay := time.Minute * time.Duration(1<<min(max(claim.Attempts-1, 0), 6))
	if delay > time.Hour {
		delay = time.Hour
	}
	result, err := tx.ExecContext(ctx, "UPDATE backup_runs SET status='failed',lease_until='',retry_at=?,error_code='backup_failed' WHERE id=? AND claim_token=? AND status='running'", now.Add(delay).UTC().Format(time.RFC3339Nano), claim.ID, claim.Token)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,'system','自动备份失败',?,'失败','')", stamp, claim.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.Notify != nil {
		s.Notify()
	}
	return nil
}

// RunDue catches up only the current local day/week/month. Unique persistent
// claims prevent repeated clock windows or a restart from creating duplicates.
func (s *BackupService) RunDue(ctx context.Context, now time.Time) error {
	done, err := s.beginStore(ctx)
	if err != nil {
		return err
	}
	defer done()
	for _, kind := range []string{"daily", "weekly", "monthly"} {
		due, err := s.claimBackup(ctx, now, kind)
		if err != nil {
			return err
		}
		if due.claim == nil && !due.complete {
			continue
		}
		if due.claim != nil {
			operation, cancel := context.WithTimeout(ctx, 90*time.Second)
			_, err = s.createLocked(operation, "system", "", due.password, kind, due.claim)
			cancel()
			due.password = ""
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return errors.Join(err, s.failedBackup(ctx, due.claim, now))
			}
		}
		if err := s.pruneBackupsLocked(ctx, kind, due.stored); err != nil {
			return err
		}
	}
	return nil
}

func (s *BackupService) pruneBackupsLocked(ctx context.Context, kind string, stored storedBackupSchedule) error {
	keep := stored.Policy.DailyKeep
	if kind == "weekly" {
		keep = stored.Policy.WeeklyKeep
	}
	if kind == "monthly" {
		keep = stored.Policy.MonthlyKeep
	}
	rows, err := s.Repository.Database.QueryContext(ctx, backupRotationQuery, kind, kind)
	if err != nil {
		return err
	}
	items := []Backup{}
	for rows.Next() {
		var item Backup
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.CreatedBy, &item.Kind, &item.FormatVersion, &item.FileBytes, &item.SHA256, &item.VerifiedAt, &item.Origin); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(items) <= keep {
		return nil
	}
	if err := s.Source.CheckConfigurationKey(); err != nil {
		return err
	}
	anchor, err := s.openChecked(ctx, items[0])
	if err != nil {
		return err
	}
	anchor.Close()
	for _, item := range items[keep:] {
		if err := s.deleteLocked(ctx, item.ID, "system", "", &stored.Revision); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) BackupRuns(ctx context.Context, page, size int) ([]BackupRun, int, error) {
	var total int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM backup_runs").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,kind,period_key,schedule_revision,status,attempts,coalesce(backup_id,''),last_attempt_at,retry_at,error_code FROM backup_runs ORDER BY last_attempt_at DESC,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []BackupRun{}
	for rows.Next() {
		var item BackupRun
		if err := rows.Scan(&item.ID, &item.Kind, &item.PeriodKey, &item.ScheduleRevision, &item.Status, &item.Attempts, &item.BackupID, &item.LastAttemptAt, &item.RetryAt, &item.ErrorCode); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

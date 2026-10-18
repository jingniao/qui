package access

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"proxycontrol/internal/platform/db"
	"time"
)

const spaceMaintenanceColumns = "attempts,unconfirmed_attempts,failures,status,mode,started_at,updated_at,last_success_at,error_code"

type SpaceMaintenanceJob struct {
	Attempts            int64  `json:"attempts"`
	UnconfirmedAttempts int64  `json:"unconfirmedAttempts"`
	Failures            int64  `json:"failures"`
	Status              string `json:"status"`
	Mode                string `json:"mode"`
	StartedAt           string `json:"startedAt"`
	UpdatedAt           string `json:"updatedAt"`
	LastSuccessAt       string `json:"lastSuccessAt"`
	ErrorCode           string `json:"errorCode"`
}

func scanSpaceMaintenance(row interface{ Scan(...any) error }) (SpaceMaintenanceJob, error) {
	var s SpaceMaintenanceJob
	if err := row.Scan(&s.Attempts, &s.UnconfirmedAttempts, &s.Failures, &s.Status, &s.Mode, &s.StartedAt, &s.UpdatedAt, &s.LastSuccessAt, &s.ErrorCode); err != nil {
		return s, err
	}
	if s.Attempts < 0 || s.Failures < 0 || s.UnconfirmedAttempts < 0 || s.Failures > s.Attempts || s.UnconfirmedAttempts > s.Attempts {
		return s, ErrInvalid
	}
	if s.Mode != "unknown" && s.Mode != "none" && s.Mode != "full" && s.Mode != "incremental" {
		return s, ErrInvalid
	}
	for _, stamp := range []string{s.StartedAt, s.UpdatedAt, s.LastSuccessAt} {
		if stamp != "" {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				return s, ErrInvalid
			}
		}
	}
	switch s.Status {
	case "idle":
		if s.Attempts != 0 || s.Failures != 0 || s.UnconfirmedAttempts != 0 || s.Mode != "unknown" || s.StartedAt != "" || s.UpdatedAt != "" || s.LastSuccessAt != "" || s.ErrorCode != "" {
			return s, ErrInvalid
		}
	case "running", "skipped", "success", "error":
		if s.Attempts < 1 || s.StartedAt == "" || s.UpdatedAt == "" {
			return s, ErrInvalid
		}
		if s.Status == "running" && (s.Mode != "unknown" || s.ErrorCode != "") {
			return s, ErrInvalid
		}
		if s.Status == "skipped" && (s.Mode != "none" && s.Mode != "full" || s.ErrorCode != "") {
			return s, ErrInvalid
		}
		if s.Status == "success" && (s.Mode != "incremental" || s.ErrorCode != "" || s.LastSuccessAt == "") {
			return s, ErrInvalid
		}
		if s.Status == "error" && (s.ErrorCode != "space_maintenance_unavailable" || s.Failures < 1) {
			return s, ErrInvalid
		}
	default:
		return s, ErrInvalid
	}
	if err := validateSpaceLifecycle(s); err != nil {
		return s, err
	}
	return s, nil
}

func validateSpaceLifecycle(s SpaceMaintenanceJob) error {
	if s.Failures < 0 || s.UnconfirmedAttempts < 0 || s.Failures > s.Attempts || s.UnconfirmedAttempts > s.Attempts-s.Failures {
		return ErrInvalid
	}
	if s.Status == "idle" {
		return nil
	}
	if s.Status != "error" && s.Failures+s.UnconfirmedAttempts >= s.Attempts {
		return ErrInvalid
	}
	parse := func(value string) (time.Time, error) {
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || at.Year() < 2000 || at.Year() > 9999 {
			return time.Time{}, ErrInvalid
		}
		return at, nil
	}
	started, err := parse(s.StartedAt)
	if err != nil {
		return err
	}
	updated, err := parse(s.UpdatedAt)
	if err != nil || updated.Before(started) {
		return ErrInvalid
	}
	if s.Status == "running" && !updated.Equal(started) {
		return ErrInvalid
	}
	if s.LastSuccessAt != "" {
		success, err := parse(s.LastSuccessAt)
		if err != nil || success.After(updated) {
			return ErrInvalid
		}
		if s.Status == "success" {
			if !success.Equal(updated) {
				return ErrInvalid
			}
		} else if success.After(started) {
			return ErrInvalid
		}
	}
	return nil
}
func (r *Repository) BeginSpaceMaintenance(ctx context.Context) (int64, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	s, err := scanSpaceMaintenance(tx.QueryRowContext(ctx, "SELECT "+spaceMaintenanceColumns+" FROM access_space_maintenance WHERE id=1"))
	if err != nil {
		return 0, err
	}
	if s.Attempts == math.MaxInt64 || s.UnconfirmedAttempts == math.MaxInt64 {
		return 0, ErrInvalid
	}
	if s.Status == "running" {
		s.UnconfirmedAttempts++
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	next := s
	next.Attempts, next.Status, next.Mode, next.StartedAt, next.UpdatedAt, next.ErrorCode = s.Attempts+1, "running", "unknown", now, now, ""
	if err := validateSpaceLifecycle(next); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE access_space_maintenance SET attempts=?,unconfirmed_attempts=?,status='running',mode='unknown',started_at=?,updated_at=?,error_code='' WHERE id=1", s.Attempts+1, s.UnconfirmedAttempts, now, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return s.Attempts + 1, nil
}
func (r *Repository) FinishSpaceMaintenance(ctx context.Context, attempt int64, mode string, failed bool) error {
	if attempt < 1 || mode != "unknown" && mode != "none" && mode != "full" && mode != "incremental" || !failed && mode == "unknown" {
		return ErrInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s, err := scanSpaceMaintenance(tx.QueryRowContext(ctx, "SELECT "+spaceMaintenanceColumns+" FROM access_space_maintenance WHERE id=1"))
	if err != nil {
		return err
	}
	status, code := "success", ""
	if mode != "incremental" {
		status = "skipped"
	}
	if failed {
		status, code = "error", "space_maintenance_unavailable"
	}
	if s.Attempts != attempt {
		return errors.New("space maintenance attempt superseded")
	}
	if s.Status != "running" {
		if s.Status == status && s.Mode == mode && s.ErrorCode == code {
			return nil
		}
		return errors.New("space maintenance result conflict")
	}
	if failed && s.Failures == math.MaxInt64 {
		return ErrInvalid
	}
	if failed {
		s.Failures++
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if status == "success" {
		s.LastSuccessAt = now
	}
	s.Status, s.Mode, s.UpdatedAt, s.ErrorCode = status, mode, now, code
	if err := validateSpaceLifecycle(s); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE access_space_maintenance SET failures=?,status=?,mode=?,updated_at=?,last_success_at=?,error_code=? WHERE id=1", s.Failures, status, mode, now, s.LastSuccessAt, code); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) preserveSpaceMaintenanceRestoreTx(ctx context.Context, tx *sql.Tx) error {
	var present, applied int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.sqlite_master WHERE type='table' AND name='access_space_maintenance'").Scan(&present); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.schema_migrations WHERE version='0057_access_space_maintenance.sql'").Scan(&applied); err != nil {
		return err
	}
	if present == 0 && applied == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM access_space_maintenance"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO access_space_maintenance(id) VALUES(1)")
		return err
	}
	if present != 1 || applied != 1 {
		return ErrInvalid
	}
	if _, err := scanSpaceMaintenance(tx.QueryRowContext(ctx, "SELECT "+spaceMaintenanceColumns+" FROM restore_source.access_space_maintenance WHERE id=1")); err != nil {
		return err
	}
	return db.CopyRecoveryTable(ctx, tx, "access_space_maintenance", "id,"+spaceMaintenanceColumns)
}

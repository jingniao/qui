package access

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
	"time"
)

const retentionBatchRows = 500

type RetentionJob struct {
	Generation    int64     `json:"generation"`
	Phase         int       `json:"phase"`
	Status        string    `json:"status"`
	Policy        Retention `json:"policy"`
	CutoffAt      string    `json:"cutoffAt"`
	StartedAt     string    `json:"startedAt"`
	UpdatedAt     string    `json:"updatedAt"`
	LastSuccessAt string    `json:"lastSuccessAt"`
	DeletedRun    int64     `json:"deletedRun"`
	DeletedTotal  int64     `json:"deletedTotal"`
	Failures      int64     `json:"failures"`
	ErrorCode     string    `json:"errorCode"`
	BatchRows     int       `json:"batchRows"`
}

const retentionJobColumns = "generation,phase,status,policy_json,cutoff_at,started_at,updated_at,last_success_at,deleted_run,deleted_total,failures,error_code"

func scanRetentionJob(row interface{ Scan(...any) error }) (RetentionJob, error) {
	result := RetentionJob{BatchRows: retentionBatchRows}
	var policy string
	err := row.Scan(&result.Generation, &result.Phase, &result.Status, &policy, &result.CutoffAt, &result.StartedAt, &result.UpdatedAt, &result.LastSuccessAt, &result.DeletedRun, &result.DeletedTotal, &result.Failures, &result.ErrorCode)
	if err != nil {
		return result, err
	}
	if result.Generation < 0 || result.Phase < 0 || result.Phase > 5 || result.DeletedRun < 0 || result.DeletedTotal < result.DeletedRun || result.Failures < 0 || len(policy) > 1024 {
		return result, ErrInvalid
	}
	switch result.Status {
	case "idle":
		if result.Generation != 0 || result.Phase != 0 || result.DeletedRun != 0 || result.DeletedTotal != 0 || result.StartedAt != "" || result.CutoffAt != "" || result.LastSuccessAt != "" || policy != "{}" {
			return result, ErrInvalid
		}
	case "running", "error":
		if result.Phase >= 5 || result.Generation < 1 {
			return result, ErrInvalid
		}
	case "success":
		if result.Phase != 5 || result.Generation < 1 {
			return result, ErrInvalid
		}
	default:
		return result, ErrInvalid
	}
	if result.Generation > 0 {
		decoder := json.NewDecoder(bytes.NewReader([]byte(policy)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&result.Policy) != nil || decoder.Decode(&struct{}{}) != io.EOF || result.Policy.Validate() != nil {
			return result, ErrInvalid
		}
		for _, stamp := range []string{result.CutoffAt, result.StartedAt, result.UpdatedAt} {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				return result, ErrInvalid
			}
		}
	}
	if result.LastSuccessAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, result.LastSuccessAt); err != nil {
			return result, ErrInvalid
		}
	}
	if result.ErrorCode != "" && result.ErrorCode != "cleanup_storage_unavailable" {
		return result, ErrInvalid
	}
	if err := validateRetentionLifecycle(result); err != nil {
		return result, err
	}
	return result, nil
}

func validateRetentionLifecycle(state RetentionJob) error {
	if state.ErrorCode != "" && state.Failures < 1 {
		return ErrInvalid
	}
	if state.Status == "error" && state.ErrorCode == "" || state.Status == "running" && state.ErrorCode != "" {
		return ErrInvalid
	}
	parse := func(value string) (time.Time, error) {
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || at.Year() < 2000 || at.Year() > 9999 {
			return time.Time{}, ErrInvalid
		}
		return at, nil
	}
	if state.Status == "idle" {
		if state.ErrorCode == "" {
			if state.Failures != 0 || state.UpdatedAt != "" {
				return ErrInvalid
			}
		} else if _, err := parse(state.UpdatedAt); err != nil {
			return err
		}
		return nil
	}
	started, err := parse(state.StartedAt)
	if err != nil {
		return err
	}
	cutoff, err := parse(state.CutoffAt)
	if err != nil || !cutoff.Equal(started) {
		return ErrInvalid
	}
	updated, err := parse(state.UpdatedAt)
	if err != nil || updated.Before(started) {
		return ErrInvalid
	}
	if state.LastSuccessAt != "" {
		success, err := parse(state.LastSuccessAt)
		if err != nil || success.After(updated) {
			return ErrInvalid
		}
		if state.Status != "success" && success.After(started) {
			return ErrInvalid
		}
		if state.Status == "success" && (success.Before(started) || state.ErrorCode == "" && !success.Equal(updated)) {
			return ErrInvalid
		}
	} else if state.Status == "success" {
		return ErrInvalid
	}
	return nil
}
func (r *Repository) RetentionJob(ctx context.Context) (RetentionJob, error) {
	return scanRetentionJob(r.Database.QueryRowContext(ctx, "SELECT "+retentionJobColumns+" FROM access_retention_job WHERE id=1"))
}

// One bounded transaction owns both deletion and its checkpoint. Retrying a
// rolled-back batch does not count or delete it twice. A policy change starts
// a fresh cutoff; already removed expired rows cannot be recreated.
func (r *Repository) AdvanceRetention(ctx context.Context, now time.Time) (pending bool, resultErr error) {
	observed := RetentionJob{Generation: -1}
	pending, resultErr = r.advanceRetentionBatch(ctx, now.UTC(), &observed)
	if resultErr != nil && !errors.Is(ctx.Err(), context.Canceled) && observed.Generation >= 0 {
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		err := r.recordRetentionFailure(recordCtx, observed)
		resultErr = errors.Join(resultErr, err)
	}
	return pending, resultErr
}
func (r *Repository) advanceRetentionBatch(ctx context.Context, now time.Time, observed ...*RetentionJob) (bool, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	policy, err := r.retentionInTx(ctx, tx)
	if err != nil {
		return false, err
	}
	state, err := scanRetentionJob(tx.QueryRowContext(ctx, "SELECT "+retentionJobColumns+" FROM access_retention_job WHERE id=1"))
	if err != nil {
		return false, err
	}
	if len(observed) > 0 && observed[0] != nil {
		*observed[0] = state
	}
	if state.Status == "idle" || state.Status == "success" || state.Policy != policy {
		if state.Generation == math.MaxInt64 {
			return false, ErrInvalid
		}
		state.Generation++
		state.Phase = 0
		state.Status = "running"
		state.Policy = policy
		state.CutoffAt = now.Format(time.RFC3339Nano)
		state.StartedAt = state.CutoffAt
		state.DeletedRun = 0
	}
	at, err := time.Parse(time.RFC3339Nano, state.CutoffAt)
	if err != nil {
		return false, err
	}
	// Five phases. A phase may cover several tables so the number of recorded
	// phases stays within the published CHECK(phase BETWEEN 0 AND 5).
	type statement struct {
		table, where string
		value        any
	}
	phases := [][]statement{
		{
			{"access_rollups", "allowed=1 AND bucket<?", timestamp(at.AddDate(0, 0, -policy.AllowedDays))},
		},
		{
			{"access_rollups", "allowed=0 AND bucket<?", timestamp(at.AddDate(0, 0, -policy.BlockedDays))},
		},
		{
			{"traffic_hourly", "bucket<?", timestamp(at.AddDate(0, 0, -policy.HourlyDays))},
		},
		{
			{"account_traffic_hourly", "bucket<?", timestamp(at.AddDate(0, 0, -policy.HourlyDays))},
		},
		{
			{"account_traffic_samples", "observed_at<?", at.AddDate(0, 0, -policy.HourlyDays).Format("2006-01-02T15:04:05.000000000Z")},
		},
	}
	phase := phases[state.Phase]
	total := int64(0)
	exhausted := true
	for _, entry := range phase {
		deleted, err := tx.ExecContext(ctx, "DELETE FROM "+entry.table+" WHERE rowid IN (SELECT rowid FROM "+entry.table+" WHERE "+entry.where+" LIMIT ?)", entry.value, retentionBatchRows)
		if err != nil {
			return false, err
		}
		rows, err := deleted.RowsAffected()
		if err != nil {
			return false, err
		}
		if rows < 0 || rows > retentionBatchRows {
			return false, ErrInvalid
		}
		total += rows
		if rows == retentionBatchRows {
			exhausted = false
		}
	}
	count := total
	if count < 0 || count > int64(retentionBatchRows*len(phase)) || state.DeletedRun > math.MaxInt64-count || state.DeletedTotal > math.MaxInt64-count {
		return false, ErrInvalid
	}
	state.DeletedRun += count
	state.DeletedTotal += count
	if exhausted {
		state.Phase++
	}
	state.Status = "running"
	state.ErrorCode = ""
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if state.Phase == 5 {
		state.Status = "success"
		state.LastSuccessAt = state.UpdatedAt
	}
	if err := validateRetentionLifecycle(state); err != nil {
		return false, err
	}
	encoded, err := json.Marshal(state.Policy)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE access_retention_job SET generation=?,phase=?,status=?,policy_json=?,cutoff_at=?,started_at=?,updated_at=?,last_success_at=?,deleted_run=?,deleted_total=?,error_code=? WHERE id=1", state.Generation, state.Phase, state.Status, string(encoded), state.CutoffAt, state.StartedAt, state.UpdatedAt, state.LastSuccessAt, state.DeletedRun, state.DeletedTotal, state.ErrorCode); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return state.Phase < 5, nil
}

// A late failure belongs to the checkpoint that this attempt actually read.
// Do not overwrite a newer batch or record an invented checkpoint after a read error.
func (r *Repository) recordRetentionFailure(ctx context.Context, expected RetentionJob) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanRetentionJob(tx.QueryRowContext(ctx, "SELECT "+retentionJobColumns+" FROM access_retention_job WHERE id=1"))
	if err != nil {
		return err
	}
	if current != expected {
		return nil
	}
	if current.Failures == math.MaxInt64 {
		return ErrInvalid
	}
	updated := time.Now().UTC().Format(time.RFC3339Nano)
	status := current.Status
	if current.Generation > 0 && current.Phase < 5 {
		status = "error"
	}
	next := current
	next.Status, next.ErrorCode, next.Failures, next.UpdatedAt = status, "cleanup_storage_unavailable", current.Failures+1, updated
	if err := validateRetentionLifecycle(next); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE access_retention_job SET status=?,error_code='cleanup_storage_unavailable',failures=?,updated_at=? WHERE id=1 AND generation=? AND phase=? AND status=? AND updated_at=? AND failures=?", status, current.Failures+1, updated, current.Generation, current.Phase, current.Status, current.UpdatedAt, current.Failures)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return ErrInvalid
	}
	return tx.Commit()
}

func (h *Handler) retentionJob(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	result, err := h.Repository.RetentionJob(r.Context())
	if err != nil {
		httpapi.Error(w, 500, "retention_status_unavailable", "清理进度暂不可读，请核对迁移和存储；不将读取失败当作清理成功")
		return
	}
	httpapi.JSON(w, 200, result)
}

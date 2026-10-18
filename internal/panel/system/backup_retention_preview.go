package system

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"time"

	"proxycontrol/internal/platform/httpapi"
)

// Shared with the actual rotation path. Only successful local automatic
// archives are candidates; imported and manual files never enter this set.
const backupRotationQuery = "SELECT b.id,b.created_at,b.created_by,b.kind,b.format_version,b.file_bytes,b.sha256,b.verified_at,b.origin FROM backups b JOIN backup_runs r ON r.backup_id=b.id WHERE r.status='success' AND r.kind=? AND b.kind=? AND b.origin='local' ORDER BY r.period_key DESC,r.last_attempt_at DESC,b.id DESC"

type BackupRetentionPreviewInput struct {
	Policy           BackupPolicy `json:"policy"`
	ExpectedRevision int64        `json:"expectedRevision"`
}

type BackupRetentionCandidate struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	FileBytes int64  `json:"fileBytes"`
}

type BackupRetentionCategory struct {
	Kind            string                     `json:"kind"`
	Keep            int                        `json:"keep"`
	Existing        int64                      `json:"existing"`
	Retained        int64                      `json:"retained"`
	Candidates      int64                      `json:"candidates"`
	CandidateBytes  int64                      `json:"candidateBytes"`
	Sample          []BackupRetentionCandidate `json:"sample"`
	SampleTruncated bool                       `json:"sampleTruncated"`
}

type BackupRetentionPreview struct {
	Policy             BackupPolicy              `json:"policy"`
	ScheduleRevision   int64                     `json:"scheduleRevision"`
	Timezone           string                    `json:"timezone"`
	ObservedAt         string                    `json:"observedAt"`
	MetadataOnly       bool                      `json:"metadataOnly"`
	ExecutionAvailable bool                      `json:"executionAvailable"`
	Candidates         int64                     `json:"candidates"`
	CandidateBytes     int64                     `json:"candidateBytes"`
	Categories         []BackupRetentionCategory `json:"categories"`
}

func (r *Repository) PreviewBackupRetention(ctx context.Context, input BackupRetentionPreviewInput) (BackupRetentionPreview, error) {
	result := BackupRetentionPreview{Policy: input.Policy, MetadataOnly: true, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Categories: []BackupRetentionCategory{}}
	if err := validateBackupPolicy(input.Policy); err != nil {
		return result, err
	}
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	stored, err := readBackupSchedule(ctx, tx)
	if err != nil {
		return result, err
	}
	if input.ExpectedRevision != stored.Revision {
		return result, ErrBackupScheduleConflict
	}
	base, err := readBase(ctx, tx)
	if err != nil {
		return result, err
	}
	result.ScheduleRevision, result.Timezone = stored.Revision, base.Timezone
	for _, kind := range []string{"daily", "weekly", "monthly"} {
		keep := input.Policy.DailyKeep
		if kind == "weekly" {
			keep = input.Policy.WeeklyKeep
		}
		if kind == "monthly" {
			keep = input.Policy.MonthlyKeep
		}
		category := BackupRetentionCategory{Kind: kind, Keep: keep, Sample: []BackupRetentionCandidate{}}
		rows, err := tx.QueryContext(ctx, backupRotationQuery, kind, kind)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var backup Backup
			if err := rows.Scan(&backup.ID, &backup.CreatedAt, &backup.CreatedBy, &backup.Kind, &backup.FormatVersion, &backup.FileBytes, &backup.SHA256, &backup.VerifiedAt, &backup.Origin); err != nil {
				rows.Close()
				return result, err
			}
			if backup.FileBytes < 0 || category.Existing == math.MaxInt64 {
				rows.Close()
				return result, ErrBackupScheduleInvalid
			}
			category.Existing++
			if !input.Policy.Enabled || category.Existing <= int64(keep) {
				category.Retained++
				continue
			}
			if math.MaxInt64-category.CandidateBytes < backup.FileBytes {
				rows.Close()
				return result, ErrBackupScheduleInvalid
			}
			category.Candidates++
			category.CandidateBytes += backup.FileBytes
			if len(category.Sample) < 10 {
				category.Sample = append(category.Sample, BackupRetentionCandidate{ID: backup.ID, CreatedAt: backup.CreatedAt, FileBytes: backup.FileBytes})
			} else {
				category.SampleTruncated = true
			}
		}
		readErr := rows.Err()
		closeErr := rows.Close()
		if readErr != nil {
			return result, readErr
		}
		if closeErr != nil {
			return result, closeErr
		}
		if math.MaxInt64-result.Candidates < category.Candidates || math.MaxInt64-result.CandidateBytes < category.CandidateBytes {
			return result, ErrBackupScheduleInvalid
		}
		result.Candidates += category.Candidates
		result.CandidateBytes += category.CandidateBytes
		result.Categories = append(result.Categories, category)
	}
	return result, tx.Commit()
}

func (h *Handler) backupRetentionPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.backupService(w) {
		return
	}
	var input BackupRetentionPreviewInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid backup retention preview request")
		return
	}
	result, err := h.Repository.PreviewBackupRetention(r.Context(), input)
	if err != nil {
		h.backupFail(w, err)
		return
	}
	httpapi.JSON(w, 200, result)
}

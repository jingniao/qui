package access

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"time"

	"proxycontrol/internal/platform/httpapi"
)

const storageCountLimit = int64(100001)

type StorageTableObservation struct {
	Kind      string `json:"kind"`
	Retention string `json:"retention"`
	Rows      int64  `json:"rows"`
	AtLeast   bool   `json:"atLeast"`
}

type StorageObservation struct {
	ObservedAt                 time.Time                 `json:"observedAt"`
	CountLimit                 int64                     `json:"countLimit"`
	MainDatabaseAllocatedBytes int64                     `json:"mainDatabaseAllocatedBytes"`
	MainDatabaseReusableBytes  int64                     `json:"mainDatabaseReusableBytes"`
	MainDatabaseReclaimMode    string                    `json:"mainDatabaseReclaimMode"`
	WarningBytes               int64                     `json:"warningBytes"`
	CriticalBytes              int64                     `json:"criticalBytes"`
	WarningLevel               string                    `json:"warningLevel"`
	Tables                     []StorageTableObservation `json:"tables"`
	Maintenance                SpaceMaintenanceJob       `json:"maintenance"`
}

// Fixed, bounded read-only observations. Replay receipts and cursors are not
// eligible for deletion merely because domain records have expired.
func (r *Repository) StorageObservation(ctx context.Context) (StorageObservation, error) {
	result := StorageObservation{CountLimit: storageCountLimit, Tables: []StorageTableObservation{}}
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	result.Maintenance, err = scanSpaceMaintenance(tx.QueryRowContext(ctx, "SELECT "+spaceMaintenanceColumns+" FROM access_space_maintenance WHERE id=1"))
	if err != nil {
		return result, err
	}
	for _, entry := range []struct{ table, kind, retention string }{
		{"access_rollups", "domain_minutes", "policy"},
		{"traffic_hourly", "domain_hours", "policy"},
		{"account_traffic_hourly", "account_hours", "policy"},
		{"account_traffic_samples", "account_samples", "policy"},
		{"traffic_daily", "domain_days", "long_term"},
		{"account_traffic_daily", "account_days", "long_term"},
		{"access_batches", "access_receipts", "replay_protection"},
		{"traffic_batches", "account_receipts", "replay_protection"},
		{"traffic_cursors", "account_cursors", "replay_protection"},
	} {
		item := StorageTableObservation{Kind: entry.kind, Retention: entry.retention}
		// Never accept a table name from input; LIMIT caps the actual row scan.
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM (SELECT 1 FROM "+entry.table+" LIMIT ?)", storageCountLimit).Scan(&item.Rows); err != nil {
			return result, err
		}
		if item.Rows < 0 || item.Rows > storageCountLimit {
			return result, ErrInvalid
		}
		item.AtLeast = item.Rows == storageCountLimit
		result.Tables = append(result.Tables, item)
	}
	var pages, free, pageSize, vacuumMode int64
	for _, entry := range []struct {
		query  string
		target *int64
	}{
		{"PRAGMA main.page_count", &pages}, {"PRAGMA main.freelist_count", &free}, {"PRAGMA main.page_size", &pageSize},
		{"PRAGMA main.auto_vacuum", &vacuumMode},
	} {
		if err := tx.QueryRowContext(ctx, entry.query).Scan(entry.target); err != nil {
			return result, err
		}
	}
	if pages < 0 || free < 0 || free > pages || pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 || pages > math.MaxInt64/pageSize {
		return result, ErrInvalid
	}
	result.MainDatabaseAllocatedBytes = pages * pageSize
	result.MainDatabaseReusableBytes = free * pageSize
	switch vacuumMode {
	case 0:
		result.MainDatabaseReclaimMode = "none"
	case 1:
		result.MainDatabaseReclaimMode = "full"
	case 2:
		result.MainDatabaseReclaimMode = "incremental"
	default:
		return result, ErrInvalid
	}
	// Advisory capacity classification only. The threshold is compared against
	// the main database allocated size, not operating-system free space, and a
	// warning is never a command: expired deletion, receipt archiving, dedup
	// protection and the free-page maintenance record remain owned by their own
	// jobs. Unconfigured thresholds stay "unknown"; an unreadable observation is
	// never displayed as "ok".
	result.WarningLevel = "unknown"
	if r.StorageThresholds != nil {
		warning, critical, err := r.StorageThresholds.StorageThresholdsInTx(ctx, tx)
		if err != nil {
			return result, err
		}
		if warning < 0 || critical < 0 || (warning == 0) != (critical == 0) || warning > 0 && critical <= warning {
			return result, ErrInvalid
		}
		result.WarningBytes, result.CriticalBytes = warning, critical
		switch {
		case warning == 0:
			result.WarningLevel = "unknown"
		case result.MainDatabaseAllocatedBytes >= critical:
			result.WarningLevel = "critical"
		case result.MainDatabaseAllocatedBytes >= warning:
			result.WarningLevel = "warning"
		default:
			result.WarningLevel = "ok"
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	result.ObservedAt = time.Now().UTC()
	return result, nil
}

func (h *Handler) storageObservation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, err := h.Repository.StorageObservation(ctx)
	if err != nil {
		httpapi.Error(w, 503, "access_storage_observation_unavailable", "访问存储规模暂不可读，请核对迁移与存储后刷新；读取失败不表示数据为空")
		return
	}
	httpapi.JSON(w, 200, result)
}

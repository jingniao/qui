package system

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"
)

var ErrStorageThresholdInvalid = errors.New("invalid storage capacity warning thresholds")
var ErrStorageThresholdConflict = errors.New("storage capacity thresholds changed; refresh before saving")

// A conservative ceiling keeps the value meaningful as bytes and rejects an
// accidental unit mix-up (for example kilobytes entered as bytes). The lower
// bound is 1 so a configured threshold is always stricter than "no limit".
const storageThresholdMax = int64(1) << 50

// StorageThresholds is advisory only. It never deletes receipts, cursors,
// accounting ledgers or identity history and never triggers maintenance. An
// unconfigured value (Revision 0) is reported as unknown instead of a fake
// safe/over state, because the operating thresholds must be set from measured
// growth before they mean anything.
type StorageThresholds struct {
	WarningBytes  int64  `json:"warningBytes"`
	CriticalBytes int64  `json:"criticalBytes"`
	Revision      int64  `json:"revision"`
	UpdatedAt     string `json:"updatedAt"`
	UpdatedBy     string `json:"updatedBy"`
}
type StorageThresholdInput struct {
	WarningBytes     int64 `json:"warningBytes"`
	CriticalBytes    int64 `json:"criticalBytes"`
	ExpectedRevision int64 `json:"expectedRevision"`
}

func validateStorageThresholds(warning, critical int64) error {
	if warning < 1 || critical <= warning || critical > storageThresholdMax {
		return ErrStorageThresholdInvalid
	}
	return nil
}

func readStorageThresholds(ctx context.Context, tx *sql.Tx) (StorageThresholds, error) {
	settings := StorageThresholds{}
	var encoded string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='storage_capacity_thresholds'").Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return settings, ErrStorageThresholdInvalid
	}
	if decoder.Decode(&struct{}{}) != io.EOF || settings.Revision < 1 {
		return settings, ErrStorageThresholdInvalid
	}
	if validateStorageThresholds(settings.WarningBytes, settings.CriticalBytes) != nil {
		return settings, ErrStorageThresholdInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, settings.UpdatedAt); err != nil {
		return settings, ErrStorageThresholdInvalid
	}
	return settings, nil
}

// StorageThresholdsInTx lets the access storage observation compute a warning
// level in its own read-only transaction without owning the settings table.
func (r *Repository) StorageThresholdsInTx(ctx context.Context, tx *sql.Tx) (int64, int64, error) {
	settings, err := readStorageThresholds(ctx, tx)
	if err != nil {
		return 0, 0, err
	}
	return settings.WarningBytes, settings.CriticalBytes, nil
}

func (r *Repository) ReadStorageThresholds(ctx context.Context) (StorageThresholds, error) {
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return StorageThresholds{}, err
	}
	defer tx.Rollback()
	settings, err := readStorageThresholds(ctx, tx)
	if err != nil {
		return settings, err
	}
	return settings, tx.Commit()
}

func (r *Repository) SaveStorageThresholds(ctx context.Context, input StorageThresholdInput, actor, ip string) (StorageThresholds, error) {
	if validateStorageThresholds(input.WarningBytes, input.CriticalBytes) != nil || input.ExpectedRevision < 0 {
		return StorageThresholds{}, ErrStorageThresholdInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return StorageThresholds{}, err
	}
	defer tx.Rollback()
	current, err := readStorageThresholds(ctx, tx)
	if err != nil {
		return current, err
	}
	if current.WarningBytes == input.WarningBytes && current.CriticalBytes == input.CriticalBytes {
		return current, nil
	}
	if current.Revision != input.ExpectedRevision || current.Revision == math.MaxInt64 {
		return current, ErrStorageThresholdConflict
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	next := StorageThresholds{WarningBytes: input.WarningBytes, CriticalBytes: input.CriticalBytes, Revision: current.Revision + 1, UpdatedAt: stamp, UpdatedBy: actor}
	data, err := json.Marshal(next)
	if err != nil {
		return next, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('storage_capacity_thresholds',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", string(data)); err != nil {
		return next, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'修改存储容量告警阈值','storage_capacity_thresholds','成功',?)", stamp, actor, ip); err != nil {
		return next, err
	}
	return next, tx.Commit()
}

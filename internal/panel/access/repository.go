package access

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"proxycontrol/internal/platform/vault"
	contract "proxycontrol/pkg/contracts/access"
	"time"
)

var ErrConflict = errors.New("access batch ID reused with different content")
var ErrInvalid = errors.New("invalid access batch")

type Repository struct {
	Vault             *vault.Vault
	Database          *sql.DB
	Settings          RetentionSource
	StorageThresholds StorageThresholdSource
}

func timestamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }
func digest(batch contract.Batch) (string, error) {
	data, err := json.Marshal(batch)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (r *Repository) Receipt(ctx context.Context, batch contract.Batch) (bool, error) {
	value, err := digest(batch)
	if err != nil {
		return false, err
	}
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	stored, _, err := r.accessReceiptTx(ctx, tx, batch.NodeID, batch.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if stored != value {
		return false, ErrConflict
	}
	return true, nil
}

// Receipt and every counter update commit together. Retries cannot add traffic
// twice, even when the connection is lost between commit and acknowledgement.
func (r *Repository) Ingest(ctx context.Context, batch contract.Batch, now time.Time) error {
	now = now.UTC()
	batch.Records = append([]contract.Rollup(nil), batch.Records...)
	if err := batch.Validate(); err != nil {
		return err
	}
	value, err := digest(batch)
	if err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	previous, _, err := r.accessReceiptTx(ctx, tx, batch.NodeID, batch.ID)
	if err == nil {
		if previous != value {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	retention, err := r.retentionInTx(ctx, tx)
	if err != nil {
		return err
	}
	for _, record := range batch.Records {
		retentionDays := retention.BlockedDays
		if record.Allowed {
			retentionDays = retention.AllowedDays
		}
		if record.Bucket.Before(now.AddDate(0, 0, -retentionDays)) {
			// Delayed telemetry still contributes exact daily traffic, but must
			// not recreate access records beyond their privacy retention window.
			if record.TrafficKnown {
				if !record.Bucket.Truncate(time.Hour).Before(now.AddDate(0, 0, -retention.HourlyDays)) {
					if err := addTraffic(ctx, tx, "traffic_hourly", timestamp(record.Bucket.Truncate(time.Hour)), record); err != nil {
						return err
					}
				}
				if err := addTraffic(ctx, tx, "traffic_daily", record.Bucket.UTC().Format("2006-01-02"), record); err != nil {
					return err
				}
			}
			continue
		}
		key := record.Key()
		var connections int64
		var upload, download sql.NullInt64
		err := tx.QueryRowContext(ctx, "SELECT connections,upload_bytes,download_bytes FROM access_rollups WHERE id=?", key).Scan(&connections, &upload, &download)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if math.MaxInt64-connections < record.Connections || upload.Valid && math.MaxInt64-upload.Int64 < record.UploadBytes || download.Valid && math.MaxInt64-download.Int64 < record.DownloadBytes {
			return errors.New("access counter overflow")
		}
		dimensions := record
		dimensions.Connections = 0
		dimensions.UploadBytes = 0
		dimensions.DownloadBytes = 0
		encoded, err := json.Marshal(dimensions)
		if err != nil {
			return err
		}
		uploadBytes, downloadBytes := measuredBytes(record)
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rollups(id,bucket,node_id,user_id,device_id,domain,target_ip,allowed,dimensions_json,connections,upload_bytes,download_bytes,traffic_known) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET connections=access_rollups.connections+excluded.connections,upload_bytes=CASE WHEN access_rollups.upload_bytes IS NULL AND excluded.upload_bytes IS NULL THEN NULL ELSE coalesce(access_rollups.upload_bytes,0)+coalesce(excluded.upload_bytes,0) END,download_bytes=CASE WHEN access_rollups.download_bytes IS NULL AND excluded.download_bytes IS NULL THEN NULL ELSE coalesce(access_rollups.download_bytes,0)+coalesce(excluded.download_bytes,0) END`, key, timestamp(record.Bucket), record.NodeID, record.UserID, record.DeviceID, record.Domain, record.TargetIP, record.Allowed, string(encoded), record.Connections, uploadBytes, downloadBytes, record.TrafficKnown); err != nil {
			return err
		}
		if record.TrafficKnown {
			if !record.Bucket.Truncate(time.Hour).Before(now.AddDate(0, 0, -retention.HourlyDays)) {
				if err := addTraffic(ctx, tx, "traffic_hourly", timestamp(record.Bucket.Truncate(time.Hour)), record); err != nil {
					return err
				}
			}
			if err := addTraffic(ctx, tx, "traffic_daily", record.Bucket.UTC().Format("2006-01-02"), record); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO access_batches(node_id,batch_id,digest,received_at) VALUES(?,?,?,?)", batch.NodeID, batch.ID, value, timestamp(now)); err != nil {
		return err
	}
	return tx.Commit()
}

// measuredBytes reports the per-domain byte counters of an access rollup. An
// unknown measurement is stored as NULL, never as an invented zero, so the page
// can distinguish "not measured" from a real zero (plan D4).
func measuredBytes(record contract.Rollup) (any, any) {
	if !record.TrafficKnown {
		return nil, nil
	}
	return record.UploadBytes, record.DownloadBytes
}

func addTraffic(ctx context.Context, tx *sql.Tx, table, bucket string, record contract.Rollup) error {
	if table != "traffic_hourly" && table != "traffic_daily" {
		return ErrInvalid
	}
	var upload, download int64
	err := tx.QueryRowContext(ctx, "SELECT upload_bytes,download_bytes FROM "+table+" WHERE bucket=? AND node_id=? AND user_id=? AND device_id=?", bucket, record.NodeID, record.UserID, record.DeviceID).Scan(&upload, &download)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if math.MaxInt64-upload < record.UploadBytes || math.MaxInt64-download < record.DownloadBytes {
		return errors.New("traffic counter overflow")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+table+"(bucket,node_id,user_id,device_id,upload_bytes,download_bytes) VALUES(?,?,?,?,?,?) ON CONFLICT(bucket,node_id,user_id,device_id) DO UPDATE SET upload_bytes="+table+".upload_bytes+excluded.upload_bytes,download_bytes="+table+".download_bytes+excluded.download_bytes", bucket, record.NodeID, record.UserID, record.DeviceID, record.UploadBytes, record.DownloadBytes)
	return err
}

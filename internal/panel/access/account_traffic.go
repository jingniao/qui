package access

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	contract "proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

func trafficDigest(batch traffic.Batch) (string, error) {
	data, err := json.Marshal(batch)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func (r *Repository) TrafficReceipt(ctx context.Context, batch traffic.Batch) (bool, error) {
	digest, err := trafficDigest(batch)
	if err != nil {
		return false, err
	}
	material, err := readCurrentTrafficReceipt(ctx, r.Database, batch.NodeID, batch.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if material.Digest != digest {
		return false, ErrConflict
	}
	return true, nil
}

func (r *Repository) IngestTraffic(ctx context.Context, batch traffic.Batch, now time.Time) error {
	now = now.UTC()
	batch.Counters = append([]traffic.Counter(nil), batch.Counters...)
	if err := batch.Validate(); err != nil {
		return err
	}
	digest, err := trafficDigest(batch)
	if err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	material, err := readCurrentTrafficReceipt(ctx, tx, batch.NodeID, batch.ID)
	if err == nil {
		if material.Digest != digest {
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
	for _, counter := range batch.Counters {
		var upload, download int64
		var observed string
		var owner, device string
		var revision int64
		err := tx.QueryRowContext(ctx, "SELECT upload_bytes,download_bytes,observed_at,user_id,device_id,revision FROM traffic_cursors WHERE node_id=? AND epoch=? AND credential_id=?", batch.NodeID, counter.CoreEpoch, counter.CredentialID).Scan(&upload, &download, &observed, &owner, &device, &revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if owner != "" && (owner != counter.UserID || device != counter.DeviceID) || revision > 0 && revision != counter.Revision {
			return errors.New("traffic counter identity or revision changed within one epoch")
		}
		if counter.UploadBytes < upload || counter.DownloadBytes < download {
			if counter.UploadBytes > upload || counter.DownloadBytes > download {
				return errors.New("traffic counter directions conflict within one epoch")
			}
			if counter.ObservedAt.UTC().Format("2006-01-02T15:04:05.000000000Z") <= observed {
				continue
			}
			return errors.New("traffic counter decreased within one epoch")
		}
		deltaUpload, deltaDownload := counter.UploadBytes-upload, counter.DownloadBytes-download
		if deltaUpload == 0 && deltaDownload == 0 {
			continue
		}
		record := contract.Rollup{NodeID: batch.NodeID, UserID: counter.UserID, DeviceID: counter.DeviceID, UploadBytes: deltaUpload, DownloadBytes: deltaDownload}
		if !counter.ObservedAt.Before(now.AddDate(0, 0, -retention.HourlyDays)) {
			if !counter.ObservedAt.Truncate(time.Hour).Before(now.AddDate(0, 0, -retention.HourlyDays)) {
				if err := addAccountTraffic(ctx, tx, "account_traffic_hourly", timestamp(counter.ObservedAt.Truncate(time.Hour)), record); err != nil {
					return err
				}
			}
			if err := addAccountSample(ctx, tx, batch.NodeID, counter, deltaUpload, deltaDownload); err != nil {
				return err
			}
		}
		if err := addAccountTraffic(ctx, tx, "account_traffic_daily", counter.ObservedAt.UTC().Format("2006-01-02"), record); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO traffic_cursors(node_id,epoch,credential_id,upload_bytes,download_bytes,observed_at,user_id,device_id,revision) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(node_id,epoch,credential_id) DO UPDATE SET upload_bytes=excluded.upload_bytes,download_bytes=excluded.download_bytes,observed_at=excluded.observed_at,user_id=excluded.user_id,device_id=excluded.device_id,revision=excluded.revision", batch.NodeID, counter.CoreEpoch, counter.CredentialID, counter.UploadBytes, counter.DownloadBytes, counter.ObservedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), counter.UserID, counter.DeviceID, counter.Revision); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO traffic_batches(node_id,batch_id,digest,received_at) VALUES(?,?,?,?)", batch.NodeID, batch.ID, digest, timestamp(now)); err != nil {
		return err
	}
	return tx.Commit()
}

// Clock resolution and offline delivery can give two rising cumulative
// observations the same timestamp. Only their cursor-proven deltas are merged;
// the batch receipt and high-water cursor still commit with the aggregation.
func addAccountSample(ctx context.Context, tx *sql.Tx, nodeID string, counter traffic.Counter, upload, download int64) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES(?,?,?,?,?,?,?,?)
 ON CONFLICT(node_id,epoch,credential_id,observed_at) DO UPDATE SET
 upload_bytes=account_traffic_samples.upload_bytes+excluded.upload_bytes,
 download_bytes=account_traffic_samples.download_bytes+excluded.download_bytes
 WHERE account_traffic_samples.user_id=excluded.user_id AND account_traffic_samples.device_id=excluded.device_id
 AND typeof(account_traffic_samples.upload_bytes)='integer' AND typeof(account_traffic_samples.download_bytes)='integer'
 AND account_traffic_samples.upload_bytes>=0 AND account_traffic_samples.download_bytes>=0
 AND account_traffic_samples.upload_bytes<=?-excluded.upload_bytes AND account_traffic_samples.download_bytes<=?-excluded.download_bytes`, nodeID, counter.CoreEpoch, counter.CredentialID, counter.ObservedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), counter.UserID, counter.DeviceID, upload, download, math.MaxInt64, math.MaxInt64)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("account sample identity or counter overflow")
	}
	return nil
}

func addAccountTraffic(ctx context.Context, tx *sql.Tx, table, bucket string, record contract.Rollup) error {
	if table != "account_traffic_hourly" && table != "account_traffic_daily" {
		return ErrInvalid
	}
	var upload, download int64
	err := tx.QueryRowContext(ctx, "SELECT upload_bytes,download_bytes FROM "+table+" WHERE bucket=? AND node_id=? AND user_id=? AND device_id=?", bucket, record.NodeID, record.UserID, record.DeviceID).Scan(&upload, &download)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if math.MaxInt64-upload < record.UploadBytes || math.MaxInt64-download < record.DownloadBytes {
		return errors.New("account traffic overflow")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+table+"(bucket,node_id,user_id,device_id,upload_bytes,download_bytes) VALUES(?,?,?,?,?,?) ON CONFLICT(bucket,node_id,user_id,device_id) DO UPDATE SET upload_bytes="+table+".upload_bytes+excluded.upload_bytes,download_bytes="+table+".download_bytes+excluded.download_bytes", bucket, record.NodeID, record.UserID, record.DeviceID, record.UploadBytes, record.DownloadBytes)
	return err
}

package user

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

// ApplyTraffic is independently idempotent because module-owned tables must
// only be changed by this repository. A retry repairs a partially completed
// cross-module operation without changing already applied counters.
func (r *Repository) ApplyTraffic(ctx context.Context, batch traffic.Batch) error {
	return r.applyTraffic(ctx, batch, nil)
}

func (r *Repository) ApplyTrafficWithStates(ctx context.Context, batch traffic.Batch, states map[int64]desiredstate.State) error {
	return r.applyTraffic(ctx, batch, states)
}
func (r *Repository) applyTraffic(ctx context.Context, batch traffic.Batch, states map[int64]desiredstate.State) error {
	if err := batch.Validate(); err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	affected := []string{}
	affectedSeen := map[string]bool{}
	for _, counter := range batch.Counters {
		cursor, err := readOriginalTrafficCursor(ctx, tx, batch.NodeID, counter.CoreEpoch, counter.CredentialID)
		upload, download := cursor.Upload, cursor.Download
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && (cursor.Owner != counter.UserID || cursor.Device != counter.DeviceID) {
			return ErrTrafficIdentity
		}
		if upload < 0 || download < 0 {
			return ErrTrafficIdentity
		}
		if counter.UploadBytes < upload || counter.DownloadBytes < download {
			if counter.UploadBytes > upload || counter.DownloadBytes > download {
				return ErrTrafficMaterial
			}
			continue
		}
		deltaUpload, deltaDownload := counter.UploadBytes-upload, counter.DownloadBytes-download
		if math.MaxInt64-deltaUpload < deltaDownload {
			return errors.New("user traffic delta overflow")
		}
		delta := deltaUpload + deltaDownload
		if delta == 0 {
			continue
		}
		var used int64
		err = tx.QueryRowContext(ctx, "SELECT CASE WHEN typeof(used_traffic)='integer' THEN used_traffic ELSE -1 END FROM users WHERE id=?", counter.UserID).Scan(&used)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && used < 0 {
			return ErrTrafficMaterial
		}
		// A user deleted after this revision was published keeps no usage row.
		// Only a tombstoned identity that the counter's own revision contains is
		// accepted; the counter still advances its cursor below, so a replay
		// stays idempotent, but there is no row left to charge.
		userDeleted := errors.Is(err, sql.ErrNoRows)
		if userDeleted {
			accepted, err := deletedIdentityAccepted(ctx, tx, states, counter)
			if err != nil {
				return err
			}
			if !accepted {
				return ErrTrafficIdentity
			}
		} else {
			if math.MaxInt64-used < delta {
				return errors.New("user traffic overflow")
			}
			update, err := tx.ExecContext(ctx, "UPDATE users SET used_traffic=? WHERE id=? AND used_traffic=?", used+delta, counter.UserID, used)
			if err != nil {
				return err
			}
			if err := requireOneTrafficUpdate(update); err != nil {
				return err
			}
			if !affectedSeen[counter.UserID] {
				affectedSeen[counter.UserID] = true
				affected = append(affected, counter.UserID)
			}
		}
		err = tx.QueryRowContext(ctx, "SELECT CASE WHEN typeof(used_traffic)='integer' THEN used_traffic ELSE -1 END FROM devices WHERE id=? AND user_id=?", counter.DeviceID, counter.UserID).Scan(&used)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && used < 0 {
			return ErrTrafficMaterial
		}
		if errors.Is(err, sql.ErrNoRows) {
			// A deleted device stops accumulating usage while its user keeps
			// accumulating the same bytes above. Without the tombstone and the
			// revision evidence it is an unknown device, not a deletion.
			if !userDeleted {
				accepted, err := deletedIdentityAccepted(ctx, tx, states, counter)
				if err != nil {
					return err
				}
				if !accepted {
					return ErrTrafficIdentity
				}
			}
		} else {
			if math.MaxInt64-used < delta {
				return errors.New("device traffic overflow")
			}
			update, err := tx.ExecContext(ctx, "UPDATE devices SET used_traffic=?,last_node_id=CASE WHEN last_online_at<=? THEN ? ELSE last_node_id END,last_online_at=CASE WHEN last_online_at<=? THEN ? ELSE last_online_at END WHERE id=? AND user_id=? AND used_traffic=?", used+delta, counter.ObservedAt.UTC().Format("2006-01-02T15:04:05Z"), batch.NodeID, counter.ObservedAt.UTC().Format("2006-01-02T15:04:05Z"), counter.ObservedAt.UTC().Format("2006-01-02T15:04:05Z"), counter.DeviceID, counter.UserID, used)
			if err != nil {
				return err
			}
			if err := requireOneTrafficUpdate(update); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO user_traffic_cursors(node_id,epoch,credential_id,user_id,device_id,upload_bytes,download_bytes) VALUES(?,?,?,?,?,?,?) ON CONFLICT(node_id,epoch,credential_id) DO UPDATE SET upload_bytes=excluded.upload_bytes,download_bytes=excluded.download_bytes", batch.NodeID, counter.CoreEpoch, counter.CredentialID, counter.UserID, counter.DeviceID, counter.UploadBytes, counter.DownloadBytes); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if r.TrafficApplied != nil && len(affected) > 0 {
		r.TrafficApplied(ctx, affected)
	}
	return nil
}

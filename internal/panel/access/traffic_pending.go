package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

const maxPendingTrafficBytes = (1 << 20) + 1024

type PendingTraffic struct {
	NodeID        string `json:"nodeId"`
	BatchID       string `json:"batchId"`
	CreatedAt     string `json:"createdAt"`
	LastAttemptAt string `json:"lastAttemptAt"`
	Attempts      int64  `json:"attempts"`
	Code          string `json:"code"`
	Digest        string `json:"digest"`
}

type pendingTrafficKey struct{ node, batch, digest string }

// Called only after the normal authenticated source checks. Original inputs
// survive the cross-module commit window; derived recovery deltas never enter.
func (r *Repository) SavePendingTraffic(ctx context.Context, batch traffic.Batch, now time.Time) error {
	if r.Vault == nil {
		return errors.New("traffic continuation encryption unavailable")
	}
	digest, err := trafficDigest(batch)
	if err != nil {
		return err
	}
	data, err := json.Marshal(batch)
	if err != nil || len(data) > 1<<20 {
		return ErrInvalid
	}
	sealed, err := r.Vault.Seal(data)
	clear(data)
	if err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var stored string
	err = tx.QueryRowContext(ctx, "SELECT digest FROM traffic_pending_batches WHERE node_id=? AND batch_id=?", batch.NodeID, batch.ID).Scan(&stored)
	if err == nil {
		if stored != digest {
			return ErrConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var count, size int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(length(encrypted_batch)),0) FROM traffic_pending_batches").Scan(&count, &size); err != nil {
		return err
	}
	if count >= 10000 || size+int64(len(sealed)) > 64<<20 {
		return errors.New("pending traffic journal capacity reached; retain node batch")
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO traffic_pending_batches(node_id,batch_id,digest,encrypted_batch,created_at) VALUES(?,?,?,?,?)", batch.NodeID, batch.ID, digest, sealed, timestamp(now)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CompletePendingTraffic(ctx context.Context, batch traffic.Batch) error {
	digest, err := trafficDigest(batch)
	if err != nil {
		return err
	}
	// The module-owned cumulative user cursor is the durable debit marker.
	// Removing the pending input is safe only after both repositories succeed.
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	material, err := readCurrentTrafficReceipt(ctx, tx, batch.NodeID, batch.ID)
	if err != nil {
		return err
	}
	stored, completed := material.Digest, material.CompletedAt
	if stored != digest {
		return ErrConflict
	}
	var pendingDigest string
	var sealed []byte
	pendingErr := tx.QueryRowContext(ctx, "SELECT CASE WHEN typeof(digest)='text' AND length(digest)=64 THEN digest ELSE '' END,CASE WHEN typeof(encrypted_batch)='blob' AND length(encrypted_batch) BETWEEN 1 AND ? THEN encrypted_batch ELSE NULL END FROM traffic_pending_batches WHERE node_id=? AND batch_id=?", maxPendingTrafficBytes, batch.NodeID, batch.ID).Scan(&pendingDigest, &sealed)
	if pendingErr != nil && !errors.Is(pendingErr, sql.ErrNoRows) {
		return pendingErr
	}
	present := pendingErr == nil
	if present {
		if pendingDigest != digest {
			return ErrConflict
		}
		if r.Vault == nil {
			return ErrReceiptMaterial
		}
		if _, err := decodePendingTraffic(r.Vault, batch.NodeID, batch.ID, digest, sealed); err != nil {
			return err
		}
	} else if completed == "" {
		return ErrReceiptMaterial
	}
	if completed != "" {
		if _, err := time.Parse(time.RFC3339Nano, completed); err != nil {
			return ErrInvalid
		}
	} else {
		completedAt := timestamp(time.Now().UTC())
		received, err := time.Parse(time.RFC3339Nano, material.ReceivedAt)
		if err != nil {
			return ErrReceiptMaterial
		}
		at, err := time.Parse(time.RFC3339Nano, completedAt)
		if err != nil || at.Before(received) {
			return ErrReceiptMaterial
		}
		updated, err := tx.ExecContext(ctx, "UPDATE traffic_batches SET accounting_completed_at=? WHERE node_id=? AND batch_id=? AND digest=? AND accounting_completed_at=''", completedAt, batch.NodeID, batch.ID, digest)
		if err != nil {
			return err
		}
		if count, err := updated.RowsAffected(); err != nil || count != 1 {
			return ErrReceiptMaterial
		}
	}
	if present {
		deleted, err := tx.ExecContext(ctx, "DELETE FROM traffic_pending_batches WHERE node_id=? AND batch_id=? AND digest=? AND encrypted_batch=?", batch.NodeID, batch.ID, digest, sealed)
		if err != nil {
			return err
		}
		if count, err := deleted.RowsAffected(); err != nil || count != 1 {
			return ErrReceiptMaterial
		}
	}
	return tx.Commit()
}

func (r *Repository) ListPendingTraffic(ctx context.Context, page, size int) ([]PendingTraffic, int, error) {
	var total int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM traffic_pending_batches").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT node_id,batch_id,created_at,last_attempt_at,attempts,last_code,CASE WHEN typeof(digest)='text' AND length(digest)=64 THEN digest ELSE '' END FROM traffic_pending_batches ORDER BY created_at,node_id,batch_id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []PendingTraffic{}
	for rows.Next() {
		var item PendingTraffic
		if err := rows.Scan(&item.NodeID, &item.BatchID, &item.CreatedAt, &item.LastAttemptAt, &item.Attempts, &item.Code, &item.Digest); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func decodePendingTraffic(key *vault.Vault, node, id, digest string, sealed []byte) (traffic.Batch, error) {
	var batch traffic.Batch
	if key == nil || len(sealed) > maxPendingTrafficBytes {
		return batch, ErrInvalid
	}
	data, err := key.Open(sealed)
	if err != nil {
		return batch, ErrInvalid
	}
	defer clear(data)
	batch, err = traffic.Decode(data)
	if err != nil || batch.NodeID != node || batch.ID != id {
		return batch, ErrInvalid
	}
	actual, err := trafficDigest(batch)
	if err != nil || actual != digest {
		return batch, ErrConflict
	}
	return batch, nil
}

// Startup and bounded background passes resume already authenticated inputs.
// No source identity, budget authorization or module-owned cursor is bypassed.
func (s *Service) ResumePendingTraffic(ctx context.Context, now time.Time) error {
	if s.Repository.Vault == nil {
		return nil
	}
	rows, err := s.Repository.Database.QueryContext(ctx, "SELECT node_id,batch_id,digest FROM traffic_pending_batches WHERE last_attempt_at<=? ORDER BY last_attempt_at,created_at,node_id,batch_id LIMIT 16", timestamp(now.Add(-time.Minute)))
	if err != nil {
		return err
	}
	keys := []pendingTrafficKey{}
	for rows.Next() {
		var key pendingTrafficKey
		if err := rows.Scan(&key.node, &key.batch, &key.digest); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	var failures error
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return errors.Join(failures, err)
		}
		claimed, err := s.Repository.Database.ExecContext(ctx, "UPDATE traffic_pending_batches SET last_attempt_at=?,attempts=min(attempts+1,2147483647),last_code='accounting_pending' WHERE node_id=? AND batch_id=? AND digest=? AND last_attempt_at<=?", timestamp(now), key.node, key.batch, key.digest, timestamp(now.Add(-time.Minute)))
		if err != nil {
			return errors.Join(failures, err)
		}
		claimedCount, err := claimed.RowsAffected()
		if err != nil {
			return errors.Join(failures, err)
		}
		if claimedCount == 0 {
			continue
		}
		var sealed []byte
		err = s.Repository.Database.QueryRowContext(ctx, "SELECT CASE WHEN length(encrypted_batch)<=? THEN encrypted_batch END FROM traffic_pending_batches WHERE node_id=? AND batch_id=? AND digest=?", maxPendingTrafficBytes, key.node, key.batch, key.digest).Scan(&sealed)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return errors.Join(failures, err)
		}
		batch, decodeErr := decodePendingTraffic(s.Repository.Vault, key.node, key.batch, key.digest, sealed)
		code := "material_invalid"
		if decodeErr == nil {
			code = "accounting_pending"
			decodeErr = s.recordDecodedTraffic(ctx, batch, true)
			var stageError *trafficProcessingError
			if errors.As(decodeErr, &stageError) {
				code = stageError.code
			}
		}
		if decodeErr == nil {
			continue
		}
		_, saveErr := s.Repository.Database.ExecContext(ctx, "UPDATE traffic_pending_batches SET last_code=? WHERE node_id=? AND batch_id=? AND digest=?", code, key.node, key.batch, key.digest)
		failures = errors.Join(failures, errors.New("traffic continuation remains pending"), saveErr)
	}
	return failures
}

// Backup authentication does not repair a missing/damaged journal.
func ValidatePendingTraffic(ctx context.Context, database *sql.DB, key *vault.Vault) error {
	var present int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='traffic_pending_batches'").Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0048_traffic_pending_batches.sql'").Scan(&present); err != nil {
			return err
		}
		if present != 0 {
			return ErrInvalid
		}
		return nil
	}
	rows, err := database.QueryContext(ctx, "SELECT node_id,batch_id,digest,CASE WHEN length(encrypted_batch)<=1049600 THEN encrypted_batch END FROM traffic_pending_batches ORDER BY node_id,batch_id")
	if err != nil {
		return err
	}
	defer rows.Close()
	var count, total int64
	for rows.Next() {
		count++
		var node, id, digest string
		var sealed []byte
		if err := rows.Scan(&node, &id, &digest, &sealed); err != nil {
			return err
		}
		total += int64(len(sealed))
		if count > 10000 || total > 64<<20 {
			return ErrInvalid
		}
		if _, err := decodePendingTraffic(key, node, id, digest, sealed); err != nil {
			return err
		}
	}
	return rows.Err()
}

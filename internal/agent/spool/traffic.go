package spool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/traffic"
)

type cursor struct{ Upload, Download int64 }

// SaveTraffic atomically persists an immutable sample and its local high-water
// cursor. A failed disk write never advances the cursor or resets the core.
func (s *Spool) SaveTraffic(ctx context.Context, counters []traffic.Counter) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.accountingLock()
	defer unlock()
	if len(counters) == 0 {
		return nil
	}
	counters = append([]traffic.Counter(nil), counters...)
	if err := traffic.ValidateSample(s.nodeID, counters); err != nil {
		return err
	}
	tx, err := s.database.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.verifyBound(ctx, tx); err != nil {
		return err
	}
	changed := []traffic.Counter{}
	for _, counter := range counters {
		var payload []byte
		previous := cursor{}
		err := tx.QueryRowContext(ctx, "SELECT payload FROM traffic_cursors WHERE epoch=? AND credential_id=?", counter.CoreEpoch, counter.CredentialID).Scan(&payload)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			data, err := s.vault.Open(payload)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &previous); err != nil {
				return err
			}
		}
		if counter.UploadBytes < previous.Upload || counter.DownloadBytes < previous.Download {
			return errors.New("core counter decreased within one process epoch")
		}
		if counter.UploadBytes == previous.Upload && counter.DownloadBytes == previous.Download {
			continue
		}
		changed = append(changed, counter)
		data, err := json.Marshal(cursor{Upload: counter.UploadBytes, Download: counter.DownloadBytes})
		if err != nil {
			return err
		}
		sealed, err := s.vault.Seal(data)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO traffic_cursors(epoch,credential_id,payload) VALUES(?,?,?) ON CONFLICT(epoch,credential_id) DO UPDATE SET payload=excluded.payload", counter.CoreEpoch, counter.CredentialID, sealed); err != nil {
			return err
		}
	}
	if len(changed) == 0 {
		return nil
	}
	type queuedBatch struct {
		id      string
		payload []byte
	}
	queue := []queuedBatch{}
	var added int64
	for offset := 0; offset < len(changed); offset += traffic.MaxBatchCounters {
		end := min(offset+traffic.MaxBatchCounters, len(changed))
		batchID, err := id.New()
		if err != nil {
			return err
		}
		batch := traffic.Batch{ID: batchID, NodeID: s.nodeID, Counters: changed[offset:end]}
		if err := batch.Validate(); err != nil {
			return err
		}
		data, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		if len(data) > 1<<20 {
			return errors.New("traffic batch exceeds protocol payload budget")
		}
		sealed, err := s.vault.Seal(data)
		if err != nil {
			return err
		}
		added += int64(len(sealed))
		if added > s.limit {
			return ErrFull
		}
		queue = append(queue, queuedBatch{id: batchID, payload: sealed})
	}
	if err := s.makeTrafficRoom(ctx, tx, added); err != nil {
		return err
	}
	for _, batch := range queue {
		if _, err := tx.ExecContext(ctx, "INSERT INTO traffic_batches(id,payload) VALUES(?,?)", batch.id, batch.payload); err != nil {
			return err
		}
	}
	return s.commitAccounting(ctx, tx)
}

func (s *Spool) PendingTraffic(ctx context.Context) (*traffic.Batch, error) {
	var payload []byte
	var batchID string
	err := s.database.QueryRowContext(ctx, "SELECT id,CASE WHEN typeof(payload)='blob' AND length(payload) BETWEEN 1 AND ? THEN payload ELSE NULL END FROM traffic_batches ORDER BY rowid LIMIT 1", maxQueuedBatchBytes).Scan(&batchID, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, invalidQueuedBatch(errors.New("traffic queue payload exceeds bounds; preserve original batch"))
	}
	data, err := s.vault.Open(payload)
	if err != nil {
		return nil, invalidQueuedBatch(err)
	}
	batch, err := traffic.Decode(data)
	if err != nil {
		return nil, invalidQueuedBatch(err)
	}
	if batch.ID != batchID || batch.NodeID != s.nodeID {
		return nil, invalidQueuedBatch(errors.New("traffic queue identity mismatch"))
	}
	return &batch, nil
}
func (s *Spool) AckTraffic(ctx context.Context, batchID string, sent ...[]byte) error {
	if batchID == "" || len(batchID) > 128 {
		return errors.New("invalid traffic acknowledgement")
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.validateQueuedAckTx(ctx, tx, "traffic", batchID, sent); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM traffic_batches WHERE id=?", batchID); err != nil {
		return err
	}
	return tx.Commit()
}

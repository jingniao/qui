package spool

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/traffic"
)

func (s *Spool) canonicalAckPayload(kind, identity string, data []byte) ([]byte, error) {
	var value any
	switch kind {
	case "access":
		batch, err := access.DecodeBatch(data)
		if err != nil || batch.ID != identity || batch.NodeID != s.nodeID {
			return nil, ErrQueuedBatchInvalid
		}
		value = batch
	case "traffic":
		batch, err := traffic.Decode(data)
		if err != nil || batch.ID != identity || batch.NodeID != s.nodeID {
			return nil, ErrQueuedBatchInvalid
		}
		value = batch
	default:
		return nil, ErrQueuedBatchInvalid
	}
	return json.Marshal(value)
}

// The production sender supplies the actual sent typed payload. The original
// row is authenticated and compared inside the transaction that removes/ACKs it.
func (s *Spool) validateQueuedAckTx(ctx context.Context, tx *sql.Tx, kind, identity string, sent [][]byte) error {
	if len(sent) == 0 {
		return nil // Retain the ID-only API for existing local callers.
	}
	if len(sent) != 1 {
		return ErrQueuedBatchInvalid
	}
	expected, err := s.canonicalAckPayload(kind, identity, sent[0])
	if err != nil {
		return err
	}
	query := ""
	switch kind {
	case "access":
		query = "SELECT " + criticalPayloadSQL + " FROM batches WHERE id=?"
	case "traffic":
		query = "SELECT " + criticalPayloadSQL + " FROM traffic_batches WHERE id=?"
	default:
		return ErrQueuedBatchInvalid
	}
	var sealed []byte
	if err := tx.QueryRowContext(ctx, query, maxQueuedBatchBytes, identity).Scan(&sealed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // Already removed, or an allowed access batch was evicted.
		}
		return err
	}
	plain, err := s.vault.Open(sealed)
	if err != nil {
		return invalidQueuedBatch(err)
	}
	defer clear(plain)
	original, err := s.canonicalAckPayload(kind, identity, plain)
	if err != nil || !bytes.Equal(original, expected) {
		return invalidQueuedBatch(errors.New("acknowledged payload differs from original queued material"))
	}
	return nil
}

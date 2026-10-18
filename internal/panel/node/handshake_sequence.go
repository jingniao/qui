package node

import (
	"context"
	"database/sql"
	"errors"
)

func (r *Repository) HandshakeSequence(ctx context.Context, nodeID string, ack uint64) (uint64, error) {
	if ack > uint64(1<<63-1) {
		return 0, ErrSequence
	}
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	last, err := scanSequence(tx.QueryRowContext(ctx, "SELECT "+sequenceMaterialSQL+" FROM nodes WHERE id=?", nodeID))
	if err != nil {
		return 0, err
	}
	if ack > last || last == uint64(1<<63-1) {
		return 0, ErrSequence
	}
	sequence, _, err := readAcceptedMessage(ctx, tx, "", nodeID)
	if !errors.Is(err, sql.ErrNoRows) {
		if err != nil {
			return 0, err
		}
		if uint64(sequence) != last {
			return 0, ErrSequence
		}
	}
	return last, nil
}

package access

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

var ErrReceiptMaterial = errors.New("原批次收据材料无法核验，请保留原记录并核对恢复来源；不能视为已接收或重新入账")

func receiptIdentifier(value string) bool {
	if len(value) < 1 || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func validReceiptDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

const accessReceiptSQL = "SELECT CASE WHEN typeof(digest)='text' AND length(digest)=64 THEN digest ELSE '' END,CASE WHEN typeof(received_at)='text' AND length(received_at) BETWEEN 1 AND 64 THEN received_at ELSE '' END FROM access_batches WHERE node_id=? AND batch_id=?"
const trafficReceiptSQL = "SELECT CASE WHEN typeof(digest)='text' AND length(digest)=64 THEN digest ELSE '' END,CASE WHEN typeof(received_at)='text' AND length(received_at) BETWEEN 1 AND 64 THEN received_at ELSE '' END FROM traffic_batches WHERE node_id=? AND batch_id=?"

func scanBatchReceipt(row interface{ Scan(...any) error }) (string, error) {
	var digest, received string
	if err := row.Scan(&digest, &received); err != nil {
		return "", err
	}
	if !validReceiptDigest(digest) {
		return "", ErrReceiptMaterial
	}
	stamp, err := time.Parse(time.RFC3339Nano, received)
	if err != nil || stamp.Year() < 2000 || stamp.Year() > 9999 {
		return "", ErrReceiptMaterial
	}
	return digest, nil
}

// accessReceiptTx returns the stored digest and received timestamp of one
// committed access batch. A missing batch reports sql.ErrNoRows so callers can
// distinguish a first delivery from a replay.
func (r *Repository) accessReceiptTx(ctx context.Context, tx *sql.Tx, node, id string) (string, string, error) {
	stored, err := scanBatchReceipt(tx.QueryRowContext(ctx, accessReceiptSQL, node, id))
	if err != nil {
		return "", "", err
	}
	var received string
	if err := tx.QueryRowContext(ctx, "SELECT received_at FROM access_batches WHERE node_id=? AND batch_id=?", node, id).Scan(&received); err != nil {
		return "", "", err
	}
	return stored, received, nil
}

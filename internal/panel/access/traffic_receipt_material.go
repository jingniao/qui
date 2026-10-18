package access

import (
	"context"
	"database/sql"
	"time"
)

type trafficReceiptMaterial struct{ Digest, ReceivedAt, CompletedAt string }

type receiptQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const trafficReceiptMaterialSQL = "SELECT CASE WHEN typeof(digest)='text' AND length(digest)=64 THEN digest ELSE '' END,CASE WHEN typeof(received_at)='text' AND length(received_at) BETWEEN 1 AND 64 THEN received_at ELSE '' END,CASE WHEN typeof(accounting_completed_at)='text' AND length(accounting_completed_at)<=64 THEN accounting_completed_at ELSE NULL END FROM traffic_batches WHERE node_id=? AND batch_id=?"

func readTrafficReceiptMaterial(row interface{ Scan(...any) error }) (trafficReceiptMaterial, error) {
	var material trafficReceiptMaterial
	if err := row.Scan(&material.Digest, &material.ReceivedAt, &material.CompletedAt); err != nil {
		return material, err
	}
	return material, validateTrafficReceiptMaterial(material)
}

func validateTrafficReceiptMaterial(material trafficReceiptMaterial) error {
	if !validReceiptDigest(material.Digest) {
		return ErrReceiptMaterial
	}
	received, err := time.Parse(time.RFC3339Nano, material.ReceivedAt)
	if err != nil || received.Year() < 2000 || received.Year() > 9999 {
		return ErrReceiptMaterial
	}
	if material.CompletedAt != "" {
		completed, err := time.Parse(time.RFC3339Nano, material.CompletedAt)
		if err != nil || completed.Year() < 2000 || completed.Year() > 9999 || completed.Before(received) {
			return ErrReceiptMaterial
		}
	}
	return nil
}

// readCurrentTrafficReceipt returns the stored metadata of one committed traffic
// batch. A missing batch reports sql.ErrNoRows.
func readCurrentTrafficReceipt(ctx context.Context, q receiptQuery, node, id string) (trafficReceiptMaterial, error) {
	return readTrafficReceiptMaterial(q.QueryRowContext(ctx, trafficReceiptMaterialSQL, node, id))
}

package access

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type TrafficCompletion struct {
	NodeID             string `json:"nodeId"`
	BatchID            string `json:"batchId"`
	Status             string `json:"status"`
	CompletedAt        string `json:"completedAt,omitempty"`
	Scope              string `json:"scope"`
	RecoveryPermission bool   `json:"recoveryPermission"`
}

// Only normal original-source validated processing can write this marker.
// A statistics receipt, missing pending input, or old cursor is not enough.
func (r *Repository) TrafficAccountingCompletion(ctx context.Context, nodeID, batchID, digest string) (TrafficCompletion, error) {
	return ReadTrafficAccountingCompletion(ctx, r.Database, nodeID, batchID, digest)
}

// Caller-owned query scope keeps recovery progress and material checks in the
// original transaction, without exposing or modifying the module's tables.
func ReadTrafficAccountingCompletion(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, nodeID, batchID, digest string) (TrafficCompletion, error) {
	result := TrafficCompletion{NodeID: nodeID, BatchID: batchID, Status: "receipt_not_observed", Scope: "current_central_database"}
	if !receiptIdentifier(nodeID) || !receiptIdentifier(batchID) || !validReceiptDigest(digest) {
		return result, ErrInvalid
	}
	material, err := readCurrentTrafficReceipt(ctx, q, nodeID, batchID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	stored, stamp := material.Digest, material.CompletedAt
	if !validReceiptDigest(stored) {
		return result, ErrInvalid
	}
	result.Status = "digest_conflict"
	if stored != digest {
		return result, nil
	}
	result.Status = "completion_not_recorded"
	if stamp != "" {
		parsed, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return result, ErrInvalid
		}
		result.Status = "original_pipeline_completed"
		result.CompletedAt = parsed.UTC().Format(time.RFC3339Nano)
	}
	return result, nil
}

// Fixed product validation. Legacy receipts without the new column remain
// unknown; a table/migration mismatch is corrupt, not a completed receipt.
func ValidateTrafficAccountingCompletion(ctx context.Context, database *sql.DB) error {
	var column, applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('traffic_batches') WHERE name='accounting_completed_at'").Scan(&column); err != nil {
		return err
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0059_traffic_accounting_completion.sql'").Scan(&applied); err != nil {
		return err
	}
	if column == 0 && applied == 0 {
		return nil
	}
	if column != 1 || applied != 1 {
		return ErrInvalid
	}
	var invalid int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM traffic_batches WHERE typeof(accounting_completed_at)<>'text' OR length(accounting_completed_at)>64").Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return ErrInvalid
	}
	// Stream without loading all receipts. No count is interpreted as
	// historical coverage and nothing is pruned to satisfy this check.
	rows, err := database.QueryContext(ctx, "SELECT CASE WHEN typeof(digest)='text' AND length(digest)=64 THEN digest ELSE '' END,CASE WHEN typeof(received_at)='text' AND length(received_at) BETWEEN 1 AND 64 THEN received_at ELSE '' END,CASE WHEN typeof(accounting_completed_at)='text' AND length(accounting_completed_at)<=64 THEN accounting_completed_at ELSE NULL END FROM traffic_batches WHERE accounting_completed_at<>''")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := readTrafficReceiptMaterial(rows); err != nil {
			return err
		}
	}
	return errors.Join(rows.Err(), rows.Close())
}

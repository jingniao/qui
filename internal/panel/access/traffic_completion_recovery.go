package access

import (
	"context"
	"database/sql"
)

func (r *Repository) preserveTrafficCompletionRestoreTx(ctx context.Context, tx *sql.Tx) error {
	var column, applied int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('traffic_batches','restore_source') WHERE name='accounting_completed_at'").Scan(&column); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.schema_migrations WHERE version='0059_traffic_accounting_completion.sql'").Scan(&applied); err != nil {
		return err
	}
	if column == 0 && applied == 0 {
		return nil
	}
	if column != 1 || applied != 1 {
		return ErrInvalid
	}
	rows, err := tx.QueryContext(ctx, "SELECT node_id,batch_id,CASE WHEN typeof(digest)='text' AND length(digest)=64 THEN digest ELSE '' END,CASE WHEN typeof(received_at)='text' AND length(received_at) BETWEEN 1 AND 64 THEN received_at ELSE '' END,CASE WHEN typeof(accounting_completed_at)='text' AND length(accounting_completed_at)<=64 THEN accounting_completed_at ELSE NULL END FROM restore_source.traffic_batches WHERE accounting_completed_at<>''")
	if err != nil {
		return err
	}
	// Validate streaming before the module-owned update; no receipt value from
	// an old source is inferred from absence or reconstructed from counters.
	for rows.Next() {
		var node, batch string
		var material trafficReceiptMaterial
		if err := rows.Scan(&node, &batch, &material.Digest, &material.ReceivedAt, &material.CompletedAt); err != nil {
			rows.Close()
			return err
		}
		if !receiptIdentifier(node) || !receiptIdentifier(batch) {
			rows.Close()
			return ErrInvalid
		}
		if err := validateTrafficReceiptMaterial(material); err != nil {
			rows.Close()
			return ErrInvalid
		}
	}
	readErr, closeErr := rows.Err(), rows.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	var invalid int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.traffic_batches WHERE typeof(accounting_completed_at)<>'text' OR length(accounting_completed_at)>64").Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `UPDATE traffic_batches SET accounting_completed_at=(SELECT s.accounting_completed_at FROM restore_source.traffic_batches s WHERE s.node_id=traffic_batches.node_id AND s.batch_id=traffic_batches.batch_id AND s.digest=traffic_batches.digest)
 WHERE EXISTS(SELECT 1 FROM restore_source.traffic_batches s WHERE s.node_id=traffic_batches.node_id AND s.batch_id=traffic_batches.batch_id AND s.digest=traffic_batches.digest)`)
	return err
}

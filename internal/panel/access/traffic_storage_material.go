package access

import (
	"context"
	"database/sql"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

// A published archive predating 0154 legitimately lacks these tables.
// Once present, every member of the new storage group must validate.
func ValidateTrafficStorage(ctx context.Context, database *sql.DB) error {
	var present int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='traffic_delivery_queues'").Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		return nil
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return validateTrafficStorageTx(ctx, tx, "")
}

func validateTrafficStorageTx(ctx context.Context, tx *sql.Tx, prefix string) error {
	for _, query := range []string{
		"SELECT count(*) FROM " + prefix + "core_traffic_hourly WHERE core NOT IN ('xray','sing-box','gost') OR typeof(upload_bytes)<>'integer' OR upload_bytes<0 OR typeof(download_bytes)<>'integer' OR download_bytes<0 OR typeof(samples)<>'integer' OR samples<=0 OR bucket<>strftime('%Y-%m-%dT%H:00:00Z',bucket) OR julianday(bucket) IS NULL",
		"SELECT count(*) FROM " + prefix + "traffic_delivery_queues WHERE queue_id='' OR length(queue_id)>128 OR typeof(completed_seq)<>'integer' OR completed_seq<0 OR typeof(confirmed_seq)<>'integer' OR confirmed_seq<0 OR confirmed_seq>completed_seq OR typeof(retired_seq)<>'integer' OR retired_seq<0 OR retired_seq>confirmed_seq OR typeof(candidate_seq)<>'integer' OR candidate_seq<retired_seq OR candidate_seq>confirmed_seq",
		"SELECT count(*) FROM " + prefix + "traffic_delivery_queues q WHERE q.closed_at<>'' AND NOT EXISTS(SELECT 1 FROM " + prefix + "node_identity_recoveries r WHERE r.id=q.recovery_id AND r.node_id=q.node_id AND r.status='consumed' AND r.consumed_at=q.closed_at)",
		"SELECT count(*) FROM " + prefix + "traffic_batches b LEFT JOIN " + prefix + "traffic_delivery_queues q ON q.node_id=b.node_id AND q.queue_id=b.queue_id WHERE typeof(b.delivery_seq)<>'integer' OR b.delivery_seq<0 OR (b.delivery_seq=0)<>(b.queue_id='') OR (b.delivery_seq>0 AND (q.node_id IS NULL OR q.queue_id<>b.queue_id OR b.delivery_seq<=q.retired_seq))",
		"SELECT count(*) FROM " + prefix + "traffic_pending_batches p LEFT JOIN " + prefix + "traffic_delivery_queues q ON q.node_id=p.node_id AND q.queue_id=p.queue_id WHERE typeof(p.delivery_seq)<>'integer' OR p.delivery_seq<0 OR (p.delivery_seq=0)<>(p.queue_id='') OR (p.delivery_seq>0 AND (q.node_id IS NULL OR q.queue_id<>p.queue_id OR p.delivery_seq<=q.retired_seq)) OR (p.delivery_seq=0 AND EXISTS(SELECT 1 FROM " + prefix + "traffic_delivery_queues switched WHERE switched.node_id=p.node_id))",
		"SELECT count(*) FROM " + prefix + "traffic_pending_batches p JOIN " + prefix + "traffic_batches b ON b.node_id=p.node_id AND b.batch_id=p.batch_id WHERE p.digest<>b.digest OR p.queue_id<>b.queue_id OR p.delivery_seq<>b.delivery_seq",
		"SELECT count(*) FROM " + prefix + "traffic_delivery_queues q WHERE completed_seq-retired_seq<>(SELECT count(*) FROM " + prefix + "traffic_batches b WHERE b.node_id=q.node_id AND b.queue_id=q.queue_id AND b.delivery_seq>q.retired_seq AND b.delivery_seq<=q.completed_seq AND b.accounting_completed_at<>'')",
		"SELECT count(*) FROM " + prefix + "traffic_storage_job WHERE id<>1 OR typeof(compacted_samples)<>'integer' OR compacted_samples<0 OR typeof(retired_receipts)<>'integer' OR retired_receipts<0 OR error_code NOT IN ('','traffic_storage_failed')",
	} {
		var bad int64
		if err := tx.QueryRowContext(ctx, query).Scan(&bad); err != nil {
			return err
		}
		if bad != 0 {
			return ErrTrafficDelivery
		}
	}
	var jobs int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+prefix+"traffic_storage_job").Scan(&jobs); err != nil {
		return err
	}
	if jobs != 1 {
		return ErrTrafficDelivery
	}
	rows, err := tx.QueryContext(ctx, "SELECT queue_id,candidate_seq,retired_seq,candidate_at,updated_at,closed_at,recovery_id FROM "+prefix+"traffic_delivery_queues")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var queue, at, updated, closed, recovery string
		var candidate, retired int64
		if err := rows.Scan(&queue, &candidate, &retired, &at, &updated, &closed, &recovery); err != nil {
			return err
		}
		if _, err := time.Parse(time.RFC3339Nano, updated); err != nil {
			return err
		}
		if err := (traffic.Delivery{QueueID: queue, Sequence: 1}).Validate(); err != nil {
			return err
		}
		if (closed == "") != (recovery == "") {
			return ErrTrafficDelivery
		}
		if closed != "" {
			if _, err := time.Parse(time.RFC3339Nano, closed); err != nil {
				return err
			}
		}
		if candidate > retired {
			if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

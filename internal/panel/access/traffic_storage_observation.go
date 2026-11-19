package access

import (
	"context"
	"database/sql"
)

type TrafficStorageProgress struct {
	CompactedSamples int64  `json:"compactedSamples"`
	RetiredReceipts  int64  `json:"retiredReceipts"`
	UpdatedAt        string `json:"updatedAt"`
	LastSuccessAt    string `json:"lastSuccessAt"`
	ErrorCode        string `json:"errorCode"`
}
type TrafficQueueObservation struct {
	NodeID    string `json:"nodeId"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Confirmed int64  `json:"confirmed"`
	Retired   int64  `json:"retired"`
}

func observeTrafficStorage(ctx context.Context, tx *sql.Tx, result *StorageObservation) error {
	if err := tx.QueryRowContext(ctx, "SELECT compacted_samples,retired_receipts,updated_at,last_success_at,error_code FROM traffic_storage_job WHERE id=1").Scan(&result.TrafficStorage.CompactedSamples, &result.TrafficStorage.RetiredReceipts, &result.TrafficStorage.UpdatedAt, &result.TrafficStorage.LastSuccessAt, &result.TrafficStorage.ErrorCode); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT n.id,n.name,CASE WHEN q.node_id IS NULL THEN CASE WHEN EXISTS(SELECT 1 FROM traffic_delivery_queues previous WHERE previous.node_id=n.id) THEN 'recovery_pending' ELSE 'legacy' END WHEN EXISTS(SELECT 1 FROM traffic_batches b WHERE b.node_id=n.id AND b.queue_id=q.queue_id AND b.delivery_seq>q.completed_seq) THEN 'gap' WHEN q.completed_seq>q.confirmed_seq THEN 'confirmation_pending' ELSE 'ready' END,coalesce(q.completed_seq,0),coalesce(q.confirmed_seq,0),coalesce(q.retired_seq,0) FROM nodes n LEFT JOIN traffic_delivery_queues q ON q.node_id=n.id AND q.closed_at='' WHERE n.deleted_at IS NULL ORDER BY n.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	result.TrafficQueues = []TrafficQueueObservation{}
	for rows.Next() {
		var item TrafficQueueObservation
		if err := rows.Scan(&item.NodeID, &item.Name, &item.Status, &item.Completed, &item.Confirmed, &item.Retired); err != nil {
			return err
		}
		result.TrafficQueues = append(result.TrafficQueues, item)
	}
	return rows.Err()
}

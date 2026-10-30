package spool

import (
	"context"
	"errors"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

// PendingTrafficForPeer preserves node-only batches when talking to an old
// panel, while allowing account batches behind them to drain normally.
func (s *Spool) PendingTrafficForPeer(ctx context.Context, multi bool) (*traffic.Batch, bool, error) {
	if multi {
		return s.pendingTraffic(ctx)
	}
	rows, err := s.database.QueryContext(ctx, "SELECT id,payload,quarantined FROM traffic_batches WHERE quarantined=0 OR (quarantined=1 AND quarantined_at<=?) ORDER BY quarantined,rowid", time.Now().Unix()-int64(TrafficQuarantineCooldown/time.Second))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var payload []byte
		var q int
		if err := rows.Scan(&id, &payload, &q); err != nil {
			return nil, false, err
		}
		if len(payload) == 0 || len(payload) > maxQueuedBatchBytes {
			return nil, false, invalidQueuedBatch(errors.New("traffic payload bounds"))
		}
		data, err := s.vault.Open(payload)
		if err != nil {
			return nil, false, invalidQueuedBatch(err)
		}
		batch, err := traffic.Decode(data)
		if err != nil {
			return nil, false, invalidQueuedBatch(err)
		}
		if batch.ID != id || batch.NodeID != s.nodeID {
			return nil, false, invalidQueuedBatch(errors.New("traffic identity mismatch"))
		}
		node := false
		for _, c := range batch.Counters {
			node = node || c.Kind == "node"
		}
		if !node {
			return &batch, q == 1, nil
		}
	}
	return nil, false, rows.Err()
}

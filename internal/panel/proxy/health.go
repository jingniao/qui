package proxy

import (
	"context"
	"database/sql"
	"errors"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/scheduler"
	"time"
)

func (r *Repository) RecordHealth(ctx context.Context, nodeID string, batch *pb.OutboundHealthBatch) error {
	if batch == nil || len(batch.Items) > 64 {
		return errors.New("outbound health batch too large")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	for _, item := range batch.Items {
		if item == nil || item.CheckedAt == nil || item.CheckedAt.CheckValid() != nil || item.LatencyMs < 0 || item.LatencyMs > 60000 || seen[item.OutboundId] {
			return errors.New("invalid outbound health record")
		}
		seen[item.OutboundId] = true
		var owner string
		if err := tx.QueryRowContext(ctx, "SELECT node_id FROM outbounds WHERE id=?", item.OutboundId).Scan(&owner); err != nil || owner != nodeID {
			return errors.New("outbound health node mismatch")
		}
		checked := item.CheckedAt.AsTime()
		if checked.After(time.Now().Add(time.Minute)) {
			return errors.New("outbound health timestamp in future")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO outbound_status(outbound_id,node_id,healthy,latency_ms,checked_at) VALUES(?,?,?,?,?) ON CONFLICT(outbound_id) DO UPDATE SET healthy=excluded.healthy,latency_ms=excluded.latency_ms,checked_at=excluded.checked_at WHERE excluded.checked_at>outbound_status.checked_at", item.OutboundId, nodeID, item.Healthy, item.LatencyMs, checked.UTC().Format("2006-01-02T15:04:05.000000000Z")); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *Repository) Health(ctx context.Context, outboundID string) (*scheduler.Health, error) {
	var health scheduler.Health
	var checked string
	err := r.Database.QueryRowContext(ctx, "SELECT healthy,latency_ms,checked_at FROM outbound_status WHERE outbound_id=?", outboundID).Scan(&health.Healthy, &health.LatencyMS, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	health.CheckedAt, err = time.Parse(time.RFC3339Nano, checked)
	return &health, err
}

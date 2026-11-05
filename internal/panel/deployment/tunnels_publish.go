package deployment

import (
	"context"
	"proxycontrol/internal/panel/proxy"
	"time"
)

// PublishTunnels keeps creation, pruning, advance and reverse rollback in the
// existing batch implementation. Only this adapter waits for terminal outcomes.
func (p *CanvasPublisher) PublishTunnels(ctx context.Context, name, author string, snapshots []proxy.NodeSnapshot) (proxy.CanvasPublishResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	result, err := p.PublishCanvas(ctx, name, author, snapshots)
	if err != nil {
		return result, err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		done := true
		if result.BatchID != "" {
			batch, e := p.Repository.Batch(ctx, result.BatchID)
			if e != nil {
				return result, e
			}
			done = batch.Status == "success" || batch.Status == "rolled_back" || batch.Status == "rollback_failed"
			result.Nodes = nil
			for _, item := range batch.Items {
				result.Nodes = append(result.Nodes, proxy.CanvasPublishNode{NodeID: item.NodeID, Revision: item.Revision, Status: item.Status})
			}
		} else {
			for i := range result.Nodes {
				n := &result.Nodes[i]
				status, e := p.Repository.TargetStatus(ctx, n.NodeID, n.Revision)
				if e != nil {
					return result, e
				}
				n.Status = status
				if status != "success" && status != "failed" && status != "rollback" && status != "rolled_back" {
					done = false
				}
			}
		}
		if done {
			for i := range result.Nodes {
				n := &result.Nodes[i]
				switch n.Status {
				case "success":
					n.Status = "applied"
				case "waiting":
					n.Status = "skipped"
				case "rollback_failed":
					n.Status = "failed"
				case "rollback":
					n.Status = "rolled_back"
				}
			}
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
		}
	}
}

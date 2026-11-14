package deployment

import (
	"context"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

// GostRemovable shares the removal guard without queuing a removal.
func (p *CanvasPublisher) GostRemovable(ctx context.Context, nodeID string) (bool, error) {
	tx, err := p.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var raw []byte
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT n.capabilities_json,s.revision FROM nodes n JOIN node_runtime_state s ON s.node_id=n.id WHERE n.id=?", nodeID).Scan(&raw, &revision); err != nil {
		return false, err
	}
	c := &pb.Capabilities{}
	if err = storedJSON.Unmarshal(raw, c); err != nil {
		return false, err
	}
	if node.SelectedCore(c, "gost") == nil {
		return false, nil
	}
	ids, err := p.Repository.coreInboundIDs(ctx, tx, nodeID, "gost", c.CoreName, revision)
	return len(ids) == 0 && err == nil, err
}

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
				// Batch rollback changes item status, but the original target retains
				// whether this node failed or successfully applied before a later failure.
				if n.Status == "failed" || n.Status == "rollback" || n.Status == "rolled_back" || n.Status == "rollback_failed" {
					var targetStatus, code string
					if e := p.Repository.Database.QueryRowContext(ctx, "SELECT status,result_code FROM deployment_targets WHERE node_id=? AND revision=?", n.NodeID, n.Revision).Scan(&targetStatus, &code); e != nil {
						return result, e
					}
					if targetStatus == "failed" || targetStatus == "rollback" {
						n.FailureCode = code
					}
					if targetStatus == "rollback" {
						n.AutomaticRollback = true
						n.Status = "rollback"
					}
				}
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

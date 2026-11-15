package deployment

import (
	"context"
	"database/sql"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

// CheckTunnelPublicationNodes uses the caller's preparation transaction so the
// existing deployment/update guards and the new reservation cannot interleave.
func (p *CanvasPublisher) CheckTunnelPublicationNodes(ctx context.Context, tx *sql.Tx, nodes []string) error {
	for _, nodeID := range nodes {
		if err := nodeAvailable(ctx, tx, nodeID, ""); err != nil {
			return err
		}
	}
	return nil
}

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
func (p *CanvasPublisher) PublishTunnels(ctx context.Context, batchID, name, author string, snapshots []proxy.NodeSnapshot) (proxy.CanvasPublishResult, error) {
	work := context.WithoutCancel(ctx)
	result := proxy.CanvasPublishResult{BatchID: batchID, Nodes: []proxy.CanvasPublishNode{}}
	retained, err := p.retainChangedSnapshots(work, snapshots)
	if err != nil {
		return result, err
	}
	if len(retained) == 0 {
		_, err = p.Repository.Database.ExecContext(work, "UPDATE deployment_batches SET status='success' WHERE id=? AND status='draft'", batchID)
		return result, err
	}
	if _, err = p.Repository.createBatch(work, batchID, "", name, author, retained); err != nil {
		return result, err
	}
	if err = p.Repository.StartBatch(work, batchID); err != nil {
		return result, err
	}
	return p.waitTunnelPublication(work, batchID)
}

func (p *CanvasPublisher) waitTunnelPublication(ctx context.Context, batchID string) (proxy.CanvasPublishResult, error) {
	timeout := 300 * time.Second
	if p.tunnelWaitTimeout != 0 {
		timeout = p.tunnelWaitTimeout
	}
	wait, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, done, err := p.tunnelPublicationResult(ctx, batchID)
		if err != nil || done {
			return result, err
		}
		select {
		case <-wait.Done():
			// Read once more with the live work context at the deadline boundary.
			result, _, err = p.tunnelPublicationResult(ctx, batchID)
			return result, err
		case <-ticker.C:
		}
	}
}

func (p *CanvasPublisher) tunnelPublicationResult(ctx context.Context, batchID string) (proxy.CanvasPublishResult, bool, error) {
	result := proxy.CanvasPublishResult{BatchID: batchID, Nodes: []proxy.CanvasPublishNode{}}
	batch, err := p.Repository.Batch(ctx, batchID)
	if err != nil {
		return result, false, err
	}
	done := batch.Status == "success" || batch.Status == "failed" || batch.Status == "rolled_back" || batch.Status == "rollback_failed"
	for _, item := range batch.Items {
		n := proxy.CanvasPublishNode{NodeID: item.NodeID, Revision: item.Revision, Status: item.Status}
		if !done {
			n.Status = "publishing"
			result.Nodes = append(result.Nodes, n)
			continue
		}
		if n.Status == "failed" || n.Status == "rolled_back" || n.Status == "rollback_failed" {
			var targetStatus, code string
			if err = p.Repository.Database.QueryRowContext(ctx, "SELECT status,result_code FROM deployment_targets WHERE node_id=? AND revision=?", n.NodeID, n.Revision).Scan(&targetStatus, &code); err != nil {
				return result, false, err
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
		result.Nodes = append(result.Nodes, n)
	}
	return result, done, nil
}

func (p *CanvasPublisher) TunnelPublicationResult(ctx context.Context, batchID string) (proxy.CanvasPublishResult, error) {
	result, _, err := p.tunnelPublicationResult(ctx, batchID)
	return result, err
}

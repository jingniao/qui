package deployment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/id"
)

// PublishCanvasRemoval uses the canvas publisher and waits for Agent receipts.
// Cancellation stops the caller from advancing to the next removal phase. The
// caller then waits for this phase to settle and rolls it back using the same
// persisted batch/command records, without discarding a still-running operation.
func (p *CanvasPublisher) PublishCanvasRemoval(ctx context.Context, name, author string, snapshots []proxy.NodeSnapshot) (proxy.CanvasRemovePublication, error) {
	result := proxy.CanvasRemovePublication{Previous: map[string]int64{}}
	for _, snapshot := range snapshots {
		previous, err := p.Repository.Current(ctx, snapshot.State.NodeID)
		if err != nil {
			return result, err
		}
		result.Previous[snapshot.State.NodeID] = previous
	}
	publication, err := p.PublishCanvas(ctx, name, author, snapshots)
	result.CanvasPublishResult = publication
	if err != nil {
		return result, err
	}
	err = p.waitCanvasRemoval(ctx, &result)
	return result, err
}

func (p *CanvasPublisher) waitCanvasRemoval(ctx context.Context, result *proxy.CanvasRemovePublication) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		done := true
		var failure error
		if result.BatchID != "" {
			batch, err := p.Repository.Batch(ctx, result.BatchID)
			if err != nil {
				return err
			}
			result.Nodes = []proxy.CanvasPublishNode{}
			for _, item := range batch.Items {
				result.Nodes = append(result.Nodes, proxy.CanvasPublishNode{NodeID: item.NodeID, Revision: item.Revision, Status: item.Status})
			}
			switch batch.Status {
			case "success":
			case "rolled_back", "rollback_failed":
				failure = fmt.Errorf("撤除批次 %s 状态 %s", batch.ID, batch.Status)
			case "draft":
				return errors.New("撤除批次尚未启动")
			default:
				done = false
			}
		} else {
			for i := range result.Nodes {
				n := &result.Nodes[i]
				status, err := p.Repository.TargetStatus(ctx, n.NodeID, n.Revision)
				if err != nil {
					return err
				}
				n.Status = status
				switch status {
				case "success":
				case "failed", "rollback", "rolled_back":
					failure = errors.Join(failure, fmt.Errorf("撤除节点 %s 状态 %s", n.NodeID, status))
				default:
					done = false
				}
			}
		}
		if done {
			if failure != nil {
				for _, n := range result.Nodes {
					var code string
					err := p.Repository.Database.QueryRowContext(ctx, "SELECT result_code FROM deployment_targets WHERE node_id=? AND revision=?", n.NodeID, n.Revision).Scan(&code)
					if err != nil && !errors.Is(err, sql.ErrNoRows) {
						return errors.Join(failure, err)
					}
					if code != "" {
						failure = errors.Join(failure, fmt.Errorf("%s: %s", n.NodeID, code))
					}
				}
			}
			return failure
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *CanvasPublisher) RollbackCanvasRemoval(ctx context.Context, result proxy.CanvasRemovePublication) error {
	if result.BatchID != "" {
		batch, err := p.Repository.Batch(ctx, result.BatchID)
		if err != nil {
			return err
		}
		if batch.Status == "draft" {
			_, err = p.Repository.Database.ExecContext(ctx, "DELETE FROM deployment_batches WHERE id=? AND status='draft'", batch.ID)
			return err
		}
		// A canceled HTTP request may leave the batch deploying. Its own state
		// machine owns pending work; wait before requesting reverse rollback.
		if batch.Status == "deploying" || batch.Status == "rolling_back" {
			waitErr := p.waitCanvasRemoval(ctx, &result)
			batch, err = p.Repository.Batch(ctx, result.BatchID)
			if err != nil {
				return errors.Join(waitErr, err)
			}
			if batch.Status != "success" && batch.Status != "rolled_back" && batch.Status != "rollback_failed" {
				return waitErr
			}
		}
		if batch.Status == "rolled_back" {
			return nil
		}
		if batch.Status == "rollback_failed" {
			return fmt.Errorf("撤除批次 %s 回滚失败，保留批次供重试", batch.ID)
		}
		if err = p.Repository.RollbackBatch(ctx, batch.ID); err != nil {
			return err
		}
		for {
			batch, err = p.Repository.Batch(ctx, batch.ID)
			if err != nil {
				return err
			}
			if batch.Status == "rolled_back" {
				return nil
			}
			if batch.Status == "rollback_failed" {
				return fmt.Errorf("撤除批次 %s 回滚失败", batch.ID)
			}
			if err = canvasRemovalTick(ctx); err != nil {
				return err
			}
		}
	}
	if len(result.Nodes) == 0 {
		return nil
	}
	if result.Nodes[0].Status == "draft" {
		return nil
	}
	// A one-node canvas publication has no batch. Preserve the exact previous
	// revision and use an ordinary deployment rollback command, including zero
	// for a node that had no running revision before the operation.
	if err := p.waitCanvasRemoval(ctx, &result); err != nil {
		if ctx.Err() != nil {
			return err
		}
		for _, n := range result.Nodes {
			if n.Status == "pending" {
				return err
			}
		}
	}
	for i := len(result.Nodes) - 1; i >= 0; i-- {
		n := result.Nodes[i]
		if n.Status != "success" {
			continue
		}
		previous, ok := result.Previous[n.NodeID]
		if !ok {
			return errors.New("撤除缺少先前节点修订")
		}
		commandID, err := p.rollbackCanvasRemovalNode(ctx, n.NodeID, n.Revision, previous)
		if err != nil {
			return err
		}
		for {
			var status, code string
			if err = p.Repository.Database.QueryRowContext(ctx, "SELECT status,result_code FROM deployment_commands WHERE id=?", commandID).Scan(&status, &code); err != nil {
				return err
			}
			if status == "success" {
				break
			}
			if status == "failed" {
				return fmt.Errorf("撤除节点 %s 回滚失败：%s", n.NodeID, code)
			}
			if err = canvasRemovalTick(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func canvasRemovalTick(ctx context.Context) error {
	timer := time.NewTimer(50 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *CanvasPublisher) rollbackCanvasRemovalNode(ctx context.Context, nodeID string, current, previous int64) (string, error) {
	if previous > 0 {
		actual, err := p.Repository.Current(ctx, nodeID)
		if err != nil {
			return "", err
		}
		if actual != current {
			return "", errors.New("撤除节点已有较新修订，拒绝覆盖")
		}
		command, err := p.Repository.RollbackNode(ctx, nodeID, previous)
		if err != nil {
			return "", err
		}
		return command.CommandId, nil
	}
	tx, err := p.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = nodeAvailable(ctx, tx, nodeID, ""); err != nil {
		return "", err
	}
	var actual int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", nodeID).Scan(&actual); err != nil {
		return "", err
	}
	if actual != current {
		return "", errors.New("撤除节点已有较新修订，拒绝覆盖")
	}
	commandID, err := id.New()
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO deployment_commands(id,node_id,revision,operation,status,created_at) VALUES(?,?,0,'rollback','pending',?)", commandID, nodeID, now()); err != nil {
		return "", err
	}
	return commandID, tx.Commit()
}

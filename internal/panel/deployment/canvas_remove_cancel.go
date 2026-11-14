package deployment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/id"
)

// CancelCanvasRemoval queues recovery without waiting for an offline Agent.
// Pending apply receipts become conflicting receipts, so they cannot advance
// runtime state or queue another batch item after cancellation commits.
func (p *CanvasPublisher) CancelCanvasRemoval(ctx context.Context, result proxy.CanvasRemovePublication) error {
	if result.BatchID != "" {
		batch, err := p.Repository.Batch(ctx, result.BatchID)
		if err != nil {
			return err
		}
		switch batch.Status {
		case "rolled_back":
			return nil
		case "draft":
			_, err = p.Repository.Database.ExecContext(ctx, "DELETE FROM deployment_batches WHERE id=? AND status='draft'", batch.ID)
			return err
		case "success":
			if err = p.Repository.RollbackBatch(ctx, batch.ID); err != nil {
				return err
			}
			return fmt.Errorf("撤除批次 %s 已排队回滚，待确认", batch.ID)
		case "rolling_back":
			return fmt.Errorf("撤除批次 %s 正在回滚，待确认", batch.ID)
		case "rollback_failed":
			return fmt.Errorf("撤除批次 %s 回滚失败，需在批次中重试", batch.ID)
		}
		tx, err := p.Repository.Database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var status string
		if err = tx.QueryRowContext(ctx, "SELECT status FROM deployment_batches WHERE id=?", batch.ID).Scan(&status); err != nil {
			return err
		}
		if status != "deploying" {
			return fmt.Errorf("撤除批次 %s 状态已变为 %s，请查看批次恢复状态", batch.ID, status)
		}
		var item BatchItem
		if err = tx.QueryRowContext(ctx, "SELECT position,node_id,revision,previous_revision FROM deployment_batch_items WHERE batch_id=? AND status='pending'", batch.ID).Scan(&item.Position, &item.NodeID, &item.Revision, &item.PreviousRevision); err != nil {
			return err
		}
		commandID, err := cancelCanvasRemovalTarget(ctx, tx, item.NodeID, item.Revision, item.PreviousRevision)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE deployment_batch_items SET status='rollback_pending',rollback_command_id=? WHERE batch_id=? AND position=?", commandID, batch.ID, item.Position); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE deployment_batches SET status='rolling_back' WHERE id=?", batch.ID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return fmt.Errorf("撤除批次 %s 已取消并排队回滚，待确认（命令 %s）", batch.ID, commandID)
	}
	for _, n := range result.Nodes {
		var pending string
		err := p.Repository.Database.QueryRowContext(ctx, "SELECT id FROM deployment_commands WHERE node_id=? AND status='pending' ORDER BY created_at,id LIMIT 1", n.NodeID).Scan(&pending)
		if err == nil {
			return fmt.Errorf("撤除节点 %s 回滚命令 %s 待确认", n.NodeID, pending)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		status, err := p.Repository.TargetStatus(ctx, n.NodeID, n.Revision)
		if err != nil {
			return err
		}
		if status == "draft" || status == "failed" || status == "rollback" || status == "rolled_back" {
			continue
		}
		previous, exists := result.Previous[n.NodeID]
		if !exists {
			return errors.New("撤除缺少先前节点修订")
		}
		if status == "success" {
			commandID, err := p.rollbackCanvasRemovalNode(ctx, n.NodeID, n.Revision, previous)
			if err != nil {
				return err
			}
			return fmt.Errorf("撤除节点 %s 回滚命令 %s 待确认（恢复修订 %d）", n.NodeID, commandID, previous)
		}
		tx, err := p.Repository.Database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		commandID, err := cancelCanvasRemovalTarget(ctx, tx, n.NodeID, n.Revision, previous)
		if err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return fmt.Errorf("撤除节点 %s 已取消，回滚命令 %s 待确认（恢复修订 %d）", n.NodeID, commandID, previous)
	}
	return nil
}

func cancelCanvasRemovalTarget(ctx context.Context, tx *sql.Tx, nodeID string, revision, previous int64) (string, error) {
	res, err := tx.ExecContext(ctx, "UPDATE deployment_targets SET status='failed',result_code='canvas_remove_canceled',updated_at=? WHERE node_id=? AND revision=? AND status='pending'", now(), nodeID, revision)
	if err != nil {
		return "", err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", errors.New("撤除发布状态已变化，未取消其他操作")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE config_revisions SET status='failed' WHERE id=?", revision); err != nil {
		return "", err
	}
	commandID, err := id.New()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO deployment_commands(id,node_id,revision,operation,status,created_at) VALUES(?,?,?,'rollback','pending',?)", commandID, nodeID, previous, now())
	return commandID, err
}

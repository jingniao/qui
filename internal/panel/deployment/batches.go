package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/id"
	pb "proxycontrol/pkg/contracts/agentpb"
)

type Batch struct {
	ID        string      `json:"id"`
	ChainID   string      `json:"chainId"`
	Name      string      `json:"name"`
	Author    string      `json:"author"`
	Status    string      `json:"status"`
	CreatedAt string      `json:"createdAt"`
	Items     []BatchItem `json:"items"`
}
type BatchItem struct {
	Position          int    `json:"position"`
	NodeID            string `json:"nodeId"`
	Revision          int64  `json:"revision"`
	PreviousRevision  int64  `json:"previousRevision"`
	Status            string `json:"status"`
	RollbackCommandID string `json:"rollbackCommandId,omitempty"`
}

func (r *Repository) Current(ctx context.Context, nodeID string) (int64, error) {
	var revision int64
	err := r.Database.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", nodeID).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}
func (r *Repository) Batch(ctx context.Context, batchID string) (Batch, error) {
	batch := Batch{Items: []BatchItem{}}
	err := r.Database.QueryRowContext(ctx, "SELECT id,chain_id,name,author,status,created_at FROM deployment_batches WHERE id=?", batchID).Scan(&batch.ID, &batch.ChainID, &batch.Name, &batch.Author, &batch.Status, &batch.CreatedAt)
	if err != nil {
		return batch, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT position,node_id,revision,previous_revision,status,rollback_command_id FROM deployment_batch_items WHERE batch_id=? ORDER BY position", batchID)
	if err != nil {
		return batch, err
	}
	defer rows.Close()
	for rows.Next() {
		var item BatchItem
		if err := rows.Scan(&item.Position, &item.NodeID, &item.Revision, &item.PreviousRevision, &item.Status, &item.RollbackCommandID); err != nil {
			return batch, err
		}
		batch.Items = append(batch.Items, item)
	}
	return batch, rows.Err()
}
func (r *Repository) Batches(ctx context.Context, page, size int) ([]Batch, int, error) {
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM deployment_batches").Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM deployment_batches ORDER BY created_at DESC,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	batches := []Batch{}
	for _, id := range ids {
		batch, err := r.Batch(ctx, id)
		if err != nil {
			return nil, 0, err
		}
		batches = append(batches, batch)
	}
	return batches, count, nil
}

// CreateBatch stores one draft revision per captured node snapshot, in
// publication order. Each revision keeps the inbound intent that belongs to its
// node (see proxy.NodeSnapshot), exactly like a single-node publication, so an
// automatic quota revision can re-enable an inbound the Owner wants enabled even
// when this batch was the node's last publication.
func (r *Repository) CreateBatch(ctx context.Context, chainID, name, author string, snapshots []proxy.NodeSnapshot) (Batch, error) {
	if err := validateBatchDependencies(snapshots); err != nil {
		return Batch{}, err
	}
	if len(snapshots) < 2 || len(snapshots) > 64 {
		return Batch{}, errors.New("batch must contain two to 64 dependency node snapshots")
	}
	batchID, err := id.New()
	if err != nil {
		return Batch{}, err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Batch{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at) VALUES(?,?,?,?,'draft',?)", batchID, chainID, name, author, now()); err != nil {
		return Batch{}, err
	}
	seen := map[string]bool{}
	for position, snapshot := range snapshots {
		state := snapshot.State
		if seen[state.NodeID] {
			return Batch{}, errors.New("duplicate batch node")
		}
		seen[state.NodeID] = true
		var previous int64
		err := tx.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", state.NodeID).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Batch{}, err
		}
		intent, err := encodeInboundIntent(snapshot.InboundIntent)
		if err != nil {
			return Batch{}, err
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO config_revisions(author,summary,status,created_at,encrypted_state,inbound_enabled_json) VALUES(?,?,'draft',?,?,?)", author, name, now(), []byte{}, intent)
		if err != nil {
			return Batch{}, err
		}
		revision, err := result.LastInsertId()
		if err != nil {
			return Batch{}, err
		}
		state.Revision = revision
		if err := state.Validate(); err != nil {
			return Batch{}, err
		}
		data, err := json.Marshal(state)
		if err != nil {
			return Batch{}, err
		}
		if len(data) > 2<<20 {
			return Batch{}, errors.New("node snapshot exceeds 2 MiB")
		}
		sealed, err := r.Vault.Seal(data)
		if err != nil {
			return Batch{}, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE config_revisions SET encrypted_state=? WHERE id=?", sealed, revision); err != nil {
			return Batch{}, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO deployment_batch_items(batch_id,position,node_id,revision,previous_revision,status) VALUES(?,?,?,?,?,'waiting')", batchID, position, state.NodeID, revision, previous); err != nil {
			return Batch{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Batch{}, err
	}
	return r.Batch(ctx, batchID)
}
func (r *Repository) StartBatch(ctx context.Context, batchID string) error {
	batch, err := r.Batch(ctx, batchID)
	if err != nil {
		return err
	}
	if batch.Status != "draft" {
		if batch.Status == "deploying" || batch.Status == "success" {
			return nil
		}
		return errors.New("batch cannot be redeployed")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Re-read under the transaction: two concurrent requests may have read the
	// same draft before either started its transaction.
	var status string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM deployment_batches WHERE id=?", batchID).Scan(&status); err != nil {
		return err
	}
	if status != "draft" {
		if status == "deploying" || status == "success" {
			return nil
		}
		return errors.New("batch cannot be redeployed")
	}
	for _, item := range batch.Items {
		if err := nodeAvailable(ctx, tx, item.NodeID, batchID); err != nil {
			return err
		}
		var previous int64
		err = tx.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", item.NodeID).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE deployment_batch_items SET previous_revision=? WHERE batch_id=? AND position=?", previous, batchID, item.Position); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE deployment_batches SET status='deploying' WHERE id=?", batchID); err != nil {
		return err
	}
	if err := r.queueItem(ctx, tx, batchID, batch.Items[0]); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) queueItem(ctx context.Context, tx *sql.Tx, batchID string, item BatchItem) error {
	state, err := r.stateInTransaction(ctx, tx, item.Revision)
	if err != nil {
		return err
	}
	if state.NodeID != item.NodeID {
		return errors.New("batch snapshot node mismatch")
	}
	if err := r.relayTargetsInTransaction(ctx, tx, state); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(?,?,'pending',?)", item.Revision, item.NodeID, now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE config_revisions SET status='pending' WHERE id=?", item.Revision); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE deployment_batch_items SET status='pending' WHERE batch_id=? AND position=?", batchID, item.Position)
	return err
}
func (r *Repository) advanceBatch(ctx context.Context, tx *sql.Tx, revision int64, success bool) error {
	var batchID string
	var position int
	err := tx.QueryRowContext(ctx, "SELECT batch_id,position FROM deployment_batch_items WHERE revision=?", revision).Scan(&batchID, &position)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	status := "failed"
	if success {
		status = "success"
	}
	if _, err := tx.ExecContext(ctx, "UPDATE deployment_batch_items SET status=? WHERE batch_id=? AND position=?", status, batchID, position); err != nil {
		return err
	}
	if !success {
		if _, err := tx.ExecContext(ctx, "UPDATE deployment_batches SET status='rolling_back' WHERE id=?", batchID); err != nil {
			return err
		}
		return queueRollback(ctx, tx, batchID)
	}
	var next BatchItem
	err = tx.QueryRowContext(ctx, "SELECT position,node_id,revision,previous_revision,status,rollback_command_id FROM deployment_batch_items WHERE batch_id=? AND status='waiting' ORDER BY position LIMIT 1", batchID).Scan(&next.Position, &next.NodeID, &next.Revision, &next.PreviousRevision, &next.Status, &next.RollbackCommandID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, "UPDATE deployment_batches SET status='success' WHERE id=?", batchID)
		return err
	}
	if err != nil {
		return err
	}
	if err := r.queueItem(ctx, tx, batchID, next); err != nil {
		if !errors.Is(err, ErrDependency) {
			return err
		}
		// Persist the previous node's successful receipt even if the next hop
		// cannot be queued. Returning the error would undo that receipt forever.
		if _, updateErr := tx.ExecContext(ctx, "UPDATE deployment_batch_items SET status='failed' WHERE batch_id=? AND position=?", batchID, next.Position); updateErr != nil {
			return updateErr
		}
		if _, updateErr := tx.ExecContext(ctx, "UPDATE config_revisions SET status='failed' WHERE id=?", next.Revision); updateErr != nil {
			return updateErr
		}
		if _, updateErr := tx.ExecContext(ctx, "INSERT INTO deployment_targets(revision,node_id,status,result_code,updated_at) VALUES(?,?,'failed','dependency_not_ready',?)", next.Revision, next.NodeID, now()); updateErr != nil {
			return updateErr
		}
		if _, updateErr := tx.ExecContext(ctx, "UPDATE deployment_batches SET status='rolling_back' WHERE id=?", batchID); updateErr != nil {
			return updateErr
		}
		return queueRollback(ctx, tx, batchID)
	}
	return nil
}
func queueRollback(ctx context.Context, tx *sql.Tx, batchID string) error {
	var item BatchItem
	err := tx.QueryRowContext(ctx, "SELECT position,node_id,revision,previous_revision FROM deployment_batch_items WHERE batch_id=? AND status='success' ORDER BY position DESC LIMIT 1", batchID).Scan(&item.Position, &item.NodeID, &item.Revision, &item.PreviousRevision)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, "UPDATE deployment_batches SET status='rolled_back' WHERE id=?", batchID)
		return err
	}
	if err != nil {
		return err
	}
	commandID, err := id.New()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO deployment_commands(id,node_id,revision,operation,status,created_at) VALUES(?,?,?,'rollback','pending',?)", commandID, item.NodeID, item.PreviousRevision, now()); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE deployment_batch_items SET status='rollback_pending',rollback_command_id=? WHERE batch_id=? AND position=?", commandID, batchID, item.Position)
	return err
}
func (r *Repository) RollbackBatch(ctx context.Context, batchID string) error {
	batch, err := r.Batch(ctx, batchID)
	if err != nil {
		return err
	}
	if batch.Status == "rolled_back" || batch.Status == "rolling_back" {
		return nil
	}
	if batch.Status != "success" && batch.Status != "rollback_failed" {
		return errors.New("only a completed batch can be manually rolled back")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentStatus string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM deployment_batches WHERE id=?", batchID).Scan(&currentStatus); err != nil {
		return err
	}
	if currentStatus == "rolled_back" || currentStatus == "rolling_back" {
		return nil
	}
	if currentStatus != "success" && currentStatus != "rollback_failed" {
		return errors.New("batch cannot be rolled back")
	}
	for _, item := range batch.Items {
		if err := nodeAvailable(ctx, tx, item.NodeID, batchID); err != nil {
			return err
		}
		if item.Status == "success" || item.Status == "rollback_failed" {
			var current int64
			if err := tx.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", item.NodeID).Scan(&current); err != nil {
				return err
			}
			if current != item.Revision {
				return errors.New("batch node has changed since deployment; rollback would overwrite a newer configuration")
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE deployment_batch_items SET status='success' WHERE batch_id=? AND status='rollback_failed'", batchID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE deployment_batches SET status='rolling_back' WHERE id=?", batchID); err != nil {
		return err
	}
	if err := queueRollback(ctx, tx, batchID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) PendingRollback(ctx context.Context, nodeID string) (*pb.RollbackCommand, error) {
	command := &pb.RollbackCommand{}
	err := r.Database.QueryRowContext(ctx, "SELECT id,revision,EXISTS(SELECT 1 FROM node_restart_requests r WHERE r.command_id=deployment_commands.id) FROM deployment_commands WHERE node_id=? AND status='pending' ORDER BY created_at,id LIMIT 1", nodeID).Scan(&command.CommandId, &command.Revision, &command.RestartCurrent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err == nil && command.Revision > 0 {
		if _, readErr := r.State(ctx, command.Revision); readErr != nil {
			return nil, readErr
		}
	}
	return command, err
}
func (r *Repository) CommandResult(ctx context.Context, nodeID string, result *pb.ApplyResult) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner, previous string
	var revision int64
	if err := tx.QueryRowContext(ctx, "SELECT node_id,revision,status FROM deployment_commands WHERE id=?", result.CommandId).Scan(&owner, &revision, &previous); err != nil {
		return err
	}
	if owner != nodeID || revision != result.Revision {
		return errors.New("rollback command result mismatch")
	}
	status := "failed"
	if result.Success {
		status = "success"
	}
	if previous != "pending" {
		if previous == status {
			return nil
		}
		return errors.New("conflicting rollback result")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE deployment_commands SET status=?,result_code=? WHERE id=?", status, result.Code, result.CommandId); err != nil {
		return err
	}
	if result.Success {
		if revision == 0 {
			if _, err := tx.ExecContext(ctx, "DELETE FROM node_runtime_state WHERE node_id=?", nodeID); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, "INSERT INTO node_runtime_state(node_id,revision) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET revision=excluded.revision", nodeID, revision); err != nil {
				return err
			}
		}
	}
	var batchID string
	err = tx.QueryRowContext(ctx, "SELECT batch_id FROM deployment_batch_items WHERE rollback_command_id=?", result.CommandId).Scan(&batchID)
	if err == nil {
		itemStatus := "rollback_failed"
		if result.Success {
			itemStatus = "rolled_back"
		}
		if _, err := tx.ExecContext(ctx, "UPDATE deployment_batch_items SET status=? WHERE rollback_command_id=?", itemStatus, result.CommandId); err != nil {
			return err
		}
		if !result.Success {
			if _, err := tx.ExecContext(ctx, "UPDATE deployment_batches SET status='rollback_failed' WHERE id=?", batchID); err != nil {
				return err
			}
		} else if err := queueRollback(ctx, tx, batchID); err != nil {
			return err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if result.Success {
		r.notifyApplied(nodeID, "post-rollback")
	}
	return nil
}

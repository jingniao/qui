package deployment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"proxycontrol/internal/platform/id"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"time"
)

var ErrDependency = errors.New("relay dependency not ready")
var ErrNodeBusy = errors.New("node is reserved by an active deployment or rollback")

// An active batch reserves all its nodes, including nodes waiting for their
// turn. The reservation is represented by the persisted batch itself.
func nodeAvailable(ctx context.Context, tx *sql.Tx, nodeID, ownBatch string) error {
	return deploymentAvailable(ctx, tx, nodeID, ownBatch)
}

func deploymentAvailable(ctx context.Context, tx *sql.Tx, nodeID, ownBatch string) error {
	if err := agentUpdateAvailable(ctx, tx, nodeID); err != nil {
		return err
	}
	if err := coreUpdateAvailable(ctx, tx, nodeID); err != nil {
		return err
	}
	var count int
	err := tx.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM deployment_targets WHERE node_id=? AND status='pending') +
 (SELECT count(*) FROM deployment_commands WHERE node_id=? AND status='pending') +
 (SELECT count(*) FROM deployment_batch_items i JOIN deployment_batches b ON b.id=i.batch_id
  WHERE i.node_id=? AND b.id<>? AND b.status IN ('deploying','rolling_back','rollback_failed'))`, nodeID, nodeID, nodeID, ownBatch).Scan(&count)
	if err != nil {
		return err
	}
	if count != 0 {
		return ErrNodeBusy
	}
	return nil
}

func (r *Repository) stateInTransaction(ctx context.Context, tx *sql.Tx, revision int64) (desiredstate.State, error) {
	var sealed []byte
	if err := tx.QueryRowContext(ctx, "SELECT encrypted_state FROM config_revisions WHERE id=?", revision).Scan(&sealed); err != nil {
		return desiredstate.State{}, err
	}
	data, err := r.Vault.Open(sealed)
	if err != nil {
		return desiredstate.State{}, err
	}
	return desiredstate.Decode(data)
}

func (r *Repository) relayTargetsInTransaction(ctx context.Context, tx *sql.Tx, state desiredstate.State) error {
	var enabled bool
	if err := tx.QueryRowContext(ctx, "SELECT enabled FROM nodes WHERE id=?", state.NodeID).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf("%w: source node is disabled", ErrDependency)
	}
	for _, outbound := range state.Outbounds {
		if !outbound.Enabled || outbound.TargetNodeID == "" {
			continue
		}
		if err := tx.QueryRowContext(ctx, "SELECT enabled FROM nodes WHERE id=?", outbound.TargetNodeID).Scan(&enabled); err != nil {
			return err
		}
		if !enabled {
			return fmt.Errorf("%w: target node is disabled", ErrDependency)
		}
		var revision int64
		err := tx.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", outbound.TargetNodeID).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: target node must be published first", ErrDependency)
		}
		if err != nil {
			return err
		}
		target, err := r.stateInTransaction(ctx, tx, revision)
		if err != nil {
			return err
		}
		if reason := relayDependency(outbound, target); reason != "matched" {
			return fmt.Errorf("%w: published relay target mismatch (%s)", ErrDependency, reason)
		}
	}
	return nil
}

// RollbackNode accepts only a snapshot that this node successfully applied.
// Commands have independent IDs because a snapshot may be restored repeatedly.
func (r *Repository) RollbackNode(ctx context.Context, nodeID string, revision int64) (*pb.RollbackCommand, error) {
	return r.rollbackNodeOperation(ctx, nodeID, revision, false)
}
func (r *Repository) RestartNode(ctx context.Context, nodeID string, revision int64) (*pb.RollbackCommand, error) {
	return r.rollbackNodeOperation(ctx, nodeID, revision, true)
}
func (r *Repository) rollbackNodeOperation(ctx context.Context, nodeID string, revision int64, restart bool) (*pb.RollbackCommand, error) {
	if revision < 1 {
		return nil, errors.New("rollback requires a successful revision")
	}
	state, err := r.State(ctx, revision)
	if err != nil {
		return nil, err
	}
	if state.NodeID != nodeID {
		return nil, errors.New("rollback revision belongs to another node")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if restart {
		var eligible int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM nodes n JOIN node_runtime_state s ON s.node_id=n.id WHERE n.id=? AND s.revision=? AND n.enabled=1 AND n.core_restart_supported=1 AND n.status IN ('online','degraded') AND n.last_heartbeat>=?`, nodeID, revision, time.Now().UTC().Add(-45*time.Second).Format("2006-01-02T15:04:05.000000000Z")).Scan(&eligible); err != nil {
			return nil, err
		}
		if eligible != 1 {
			return nil, errors.New("restart requires a fresh capable node and unchanged current revision")
		}
	}
	// Repeated HTTP requests while this same command is pending are harmless.
	var pendingID string
	var pendingRevision int64
	err = tx.QueryRowContext(ctx, "SELECT id,revision FROM deployment_commands WHERE node_id=? AND status='pending' ORDER BY created_at,id LIMIT 1", nodeID).Scan(&pendingID, &pendingRevision)
	if err == nil && pendingRevision == revision {
		var batchCommands int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM deployment_batch_items WHERE rollback_command_id=?", pendingID).Scan(&batchCommands); err != nil {
			return nil, err
		}
		if batchCommands == 0 {
			var previousRestart int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM node_restart_requests WHERE command_id=?", pendingID).Scan(&previousRestart); err != nil {
				return nil, err
			}
			if (previousRestart != 0) != restart {
				return nil, ErrNodeBusy
			}
			return &pb.RollbackCommand{CommandId: pendingID, Revision: revision, RestartCurrent: restart}, nil
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err := nodeAvailable(ctx, tx, nodeID, ""); err != nil {
		return nil, err
	}
	var successes int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM deployment_targets WHERE node_id=? AND revision=? AND status='success'", nodeID, revision).Scan(&successes); err != nil {
		return nil, err
	}
	if successes != 1 {
		return nil, errors.New("rollback revision was not successfully applied")
	}
	if err := r.relayTargetsInTransaction(ctx, tx, state); err != nil {
		return nil, err
	}
	commandID, err := id.New()
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO deployment_commands(id,node_id,revision,operation,status,created_at) VALUES(?,?,?,'rollback','pending',?)", commandID, nodeID, revision, now()); err != nil {
		return nil, err
	}
	if restart {
		if _, err := tx.ExecContext(ctx, "INSERT INTO node_restart_requests(command_id,node_id,revision,created_at) VALUES(?,?,?,?)", commandID, nodeID, revision, now()); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &pb.RollbackCommand{CommandId: commandID, Revision: revision, RestartCurrent: restart}, nil
}

package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"time"
)

type Repository struct {
	Database *sql.DB
	Vault    *vault.Vault
	// Applied is called with the node ID and the operation name ("post-deploy"
	// or "post-rollback") after the panel recorded that operation as successful.
	// A deploy and a rollback both publish a stored revision verbatim, so a
	// revision rendered before a user was disabled (or re-enabled) would leave
	// the node's user access surface out of step with the user records. The
	// platform uses it to queue one quota reconciliation, which is a no-op when
	// the node is already in sync. Optional and nil-safe.
	Applied func(nodeID, operation string)
}
type Revision struct {
	ID         int64  `json:"id"`
	Author     string `json:"author"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	NodeID     string `json:"nodeId"`
	ResultCode string `json:"resultCode"`
	BatchID    string `json:"batchId,omitempty"`
}

func (r *Repository) TargetStatus(ctx context.Context, nodeID string, revision int64) (string, error) {
	var status string
	err := r.Database.QueryRowContext(ctx, "SELECT status FROM deployment_targets WHERE node_id=? AND revision=?", nodeID, revision).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		state, readErr := r.State(ctx, revision)
		if readErr != nil {
			return "", readErr
		}
		if state.NodeID != nodeID {
			return "", errors.New("conversion target revision belongs to another node")
		}
		return "draft", nil
	}
	return status, err
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z") }
func (r *Repository) Create(ctx context.Context, state desiredstate.State, author, summary string) (Revision, error) {
	return r.CreateWithInboundIntent(ctx, state, nil, author, summary)
}

// CreateWithInboundIntent stores a revision together with the set of inbounds
// the Owner had enabled when it was created. The renderer disables every inbound
// that no user credential reaches, so the state alone cannot tell such an
// inbound from one the Owner disabled on purpose; the Deployment module keeps
// that intent next to the revision. A nil intent records no intent at all
// (unknown), which is what a raw revision built from a caller-supplied state
// and every revision from before the column existed reports, and which an
// automatic quota revision never widens. An empty, non-nil slice records "the
// Owner wanted no inbound enabled".
func (r *Repository) CreateWithInboundIntent(ctx context.Context, state desiredstate.State, enabled []string, author, summary string) (Revision, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Revision{}, err
	}
	defer tx.Rollback()
	revision, err := r.createStateTx(ctx, tx, state, enabled, author, summary)
	if err != nil {
		return Revision{}, err
	}
	return revision, tx.Commit()
}

func (r *Repository) createStateTx(ctx context.Context, tx *sql.Tx, state desiredstate.State, enabled []string, author, summary string) (Revision, error) {
	intent, err := encodeInboundIntent(enabled)
	if err != nil {
		return Revision{}, err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO config_revisions(author,summary,status,created_at,encrypted_state,inbound_enabled_json) VALUES(?,?,'draft',?,?,?)", author, summary, now(), []byte{}, intent)
	if err != nil {
		return Revision{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Revision{}, err
	}
	state.Revision = id
	if err := state.Validate(); err != nil {
		return Revision{}, err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return Revision{}, err
	}
	if len(data) > 2<<20 {
		return Revision{}, errors.New("DesiredState exceeds 2 MiB")
	}
	sealed, err := r.Vault.Seal(data)
	if err != nil {
		return Revision{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE config_revisions SET encrypted_state=? WHERE id=?", sealed, id); err != nil {
		return Revision{}, err
	}
	return Revision{ID: id, Author: author, Summary: summary, Status: "draft", NodeID: state.NodeID}, nil
}

// encodeInboundIntent renders the intent as it is stored: NULL keeps "unknown"
// apart from an empty set, because only an explicit set may re-enable an
// inbound an automatic revision finds disabled.
func encodeInboundIntent(enabled []string) (any, error) {
	if enabled == nil {
		return nil, nil
	}
	data, err := json.Marshal(enabled)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

// InboundIntent reads the set of inbounds the Owner had enabled when a revision
// was created. A nil result means the intent is unknown: the revision predates
// the column, or it was created from a state that never carried an intent.
func (r *Repository) InboundIntent(ctx context.Context, id int64) ([]string, error) {
	var stored sql.NullString
	if err := r.Database.QueryRowContext(ctx, "SELECT inbound_enabled_json FROM config_revisions WHERE id=?", id).Scan(&stored); err != nil {
		return nil, err
	}
	if !stored.Valid {
		return nil, nil
	}
	enabled := []string{}
	if err := json.Unmarshal([]byte(stored.String), &enabled); err != nil {
		return nil, err
	}
	return enabled, nil
}

func (r *Repository) State(ctx context.Context, id int64) (desiredstate.State, error) {
	var sealed []byte
	if err := r.Database.QueryRowContext(ctx, "SELECT encrypted_state FROM config_revisions WHERE id=?", id).Scan(&sealed); err != nil {
		return desiredstate.State{}, err
	}
	data, err := r.Vault.Open(sealed)
	if err != nil {
		return desiredstate.State{}, err
	}
	return desiredstate.Decode(data)
}

func (r *Repository) Published(ctx context.Context, nodeID string) (desiredstate.State, error) {
	state, _, err := r.PublishedWithIntent(ctx, nodeID)
	return state, err
}

// PublishedWithIntent is Published plus the inbound intent recorded with the
// revision the node runs. An automatic quota revision inherits that intent
// unchanged, so it can never adopt the Owner's unpublished draft of which
// inbounds should be enabled.
func (r *Repository) PublishedWithIntent(ctx context.Context, nodeID string) (desiredstate.State, []string, error) {
	var revision int64
	err := r.Database.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", nodeID).Scan(&revision)
	if err != nil {
		return desiredstate.State{}, nil, err
	}
	state, err := r.State(ctx, revision)
	if err != nil {
		return desiredstate.State{}, nil, err
	}
	intent, err := r.InboundIntent(ctx, revision)
	if err != nil {
		return desiredstate.State{}, nil, err
	}
	return state, intent, nil
}

func (r *Repository) AppliedState(ctx context.Context, nodeID string, revision int64) (desiredstate.State, error) {
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM deployment_targets WHERE node_id=? AND revision=? AND status='success'", nodeID, revision).Scan(&count); err != nil {
		return desiredstate.State{}, err
	}
	if count != 1 {
		return desiredstate.State{}, errors.New("revision not successfully applied")
	}
	state, err := r.State(ctx, revision)
	if err == nil && state.NodeID != nodeID {
		return desiredstate.State{}, errors.New("revision node mismatch")
	}
	return state, err
}
func (r *Repository) Deploy(ctx context.Context, id int64) error {
	state, err := r.State(ctx, id)
	if err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRowContext(ctx, "SELECT status FROM deployment_targets WHERE revision=? AND node_id=?", id, state.NodeID).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var batchMember int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM deployment_batch_items WHERE revision=?", id).Scan(&batchMember); err != nil {
		return err
	}
	if batchMember != 0 {
		return errors.New("batch revisions must be published through their batch")
	}
	if err := nodeAvailable(ctx, tx, state.NodeID, ""); err != nil {
		return err
	}
	if err := r.relayTargetsInTransaction(ctx, tx, state); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(?,?,'pending',?)", id, state.NodeID, now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE config_revisions SET status='pending' WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) Pending(ctx context.Context, nodeID string) (*pb.ApplyCommand, error) {
	var id int64
	err := r.Database.QueryRowContext(ctx, "SELECT revision FROM deployment_targets WHERE node_id=? AND status='pending' ORDER BY revision LIMIT 1", nodeID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state, err := r.State(ctx, id)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return &pb.ApplyCommand{DesiredStateJson: data}, nil
}
func (r *Repository) Result(ctx context.Context, nodeID string, result *pb.ApplyResult) error {
	if result == nil {
		return errors.New("missing deployment result")
	}
	if result.CommandId != "" {
		return r.CommandResult(ctx, nodeID, result)
	}
	state, err := r.State(ctx, result.Revision)
	if err != nil {
		return err
	}
	if state.NodeID != nodeID {
		return errors.New("deployment result node mismatch")
	}
	status := "failed"
	if result.Success {
		status = "success"
	} else if result.RolledBack {
		status = "rollback"
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM deployment_targets WHERE revision=? AND node_id=?", result.Revision, nodeID).Scan(&previous); err != nil {
		return err
	}
	if previous != "pending" {
		if previous == status {
			return tx.Commit()
		}
		return errors.New("conflicting deployment result")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE deployment_targets SET status=?,result_code=?,updated_at=? WHERE revision=? AND node_id=?", status, result.Code, now(), result.Revision, nodeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE config_revisions SET status=? WHERE id=?", status, result.Revision); err != nil {
		return err
	}
	if result.Success {
		if _, err := tx.ExecContext(ctx, "INSERT INTO node_runtime_state(node_id,revision) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET revision=excluded.revision", nodeID, result.Revision); err != nil {
			return err
		}
	}
	if err := r.advanceBatch(ctx, tx, result.Revision, result.Success); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if result.Success {
		r.notifyApplied(nodeID, "post-deploy")
	}
	return nil
}

// notifyApplied tells the platform that a node now runs a new revision, so its
// user access surface can be re-checked against the user records outside this
// transaction.
func (r *Repository) notifyApplied(nodeID, operation string) {
	if r.Applied != nil && nodeID != "" {
		r.Applied(nodeID, operation)
	}
}
func (r *Repository) List(ctx context.Context, page, size int) ([]Revision, int, error) {
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM config_revisions").Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT c.id,c.author,c.summary,c.status,c.created_at,coalesce(t.node_id,b.node_id,''),coalesce(t.result_code,''),coalesce(b.batch_id,'') FROM config_revisions c LEFT JOIN deployment_targets t ON t.revision=c.id LEFT JOIN deployment_batch_items b ON b.revision=c.id ORDER BY c.id DESC LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	revisions := []Revision{}
	for rows.Next() {
		var revision Revision
		if err := rows.Scan(&revision.ID, &revision.Author, &revision.Summary, &revision.Status, &revision.CreatedAt, &revision.NodeID, &revision.ResultCode, &revision.BatchID); err != nil {
			return nil, 0, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, count, rows.Err()
}

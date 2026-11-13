package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrNodeNotEnrolled = errors.New("node is not enrolled")

type Recovery struct {
	ID                  string `json:"id"`
	NodeID              string `json:"nodeId"`
	PreviousFingerprint string `json:"previousFingerprint"`
	NewFingerprint      string `json:"newFingerprint,omitempty"`
	Owner               string `json:"owner"`
	RevokedOnIssue      bool   `json:"revokedOnIssue"`
	Status              string `json:"status"`
	RedeployState       string `json:"redeployState"`
	RedeployRevision    int64  `json:"redeployRevision"`
	RedeployCode        string `json:"redeployCode,omitempty"`
	CreatedAt           string `json:"createdAt"`
	ExpiresAt           string `json:"expiresAt"`
	ConsumedAt          string `json:"consumedAt,omitempty"`
}

func recoveryBlockers(ctx context.Context, q rowQuery, id string) ([]Blocker, error) {
	items := []Blocker{}
	queries := []struct{ kind, table, query string }{
		{"deployment_target", "deployment_targets", "SELECT CAST(revision AS TEXT),'' FROM deployment_targets WHERE node_id=? AND status='pending'"},
		{"deployment_command", "deployment_commands", "SELECT id,operation FROM deployment_commands WHERE node_id=? AND status='pending'"},
		{"deployment_batch", "deployment_batches", "SELECT DISTINCT b.id,b.name FROM deployment_batches b JOIN deployment_batch_items i ON i.batch_id=b.id WHERE i.node_id=? AND b.status IN ('deploying','rolling_back','rollback_failed')"},
		{"agent_update", "agent_update_tasks", "SELECT id,status FROM agent_update_tasks WHERE node_id=? AND status IN ('queued','running','failed_closed')"},
		{"core_update", "core_update_tasks", "SELECT id,status FROM core_update_tasks WHERE node_id=? AND status IN ('queued','running','failed_closed')"},
		{"core_remove", "core_remove_tasks", "SELECT id,component FROM core_remove_tasks WHERE node_id=? AND status IN ('queued','running')"},
		{"node_pause", "node_pause_tasks", "SELECT id,status FROM node_pause_tasks WHERE node_id=? AND status IN ('pausing','resuming')"},
	}
	for _, item := range queries {
		var present int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", item.table).Scan(&present); err != nil {
			return nil, err
		}
		if present == 0 {
			continue
		}
		rows, err := q.QueryContext(ctx, item.query, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var b Blocker
			b.Kind = item.kind
			if err := rows.Scan(&b.ID, &b.Name); err != nil {
				rows.Close()
				return nil, err
			}
			items = append(items, b)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return items, nil
}

func (r *Repository) IssueRecovery(ctx context.Context, nodeID, owner string, ttl time.Duration, revokeNow bool) (Node, string, time.Time, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)
	token, err := RandomID()
	if err != nil {
		return Node{}, "", expires, err
	}
	recoveryID, err := RandomID()
	if err != nil {
		return Node{}, "", expires, err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Node{}, "", expires, err
	}
	defer tx.Rollback()
	var n Node
	var caps, hb string
	var fingerprint, addrsJSON string
	err = tx.QueryRowContext(ctx, "SELECT "+columns+",coalesce((SELECT fingerprint FROM node_identities WHERE node_id=nodes.id),'') FROM nodes WHERE id=?", nodeID).Scan(&n.ID, &n.Name, &n.Status, &n.Enabled, &n.Hostname, &n.OS, &n.AgentVersion, &n.PublicIP, &n.OutboundSourceIP, &n.OutboundInterface, &n.HostSourceIPv4, &n.HostSourceIPv6, &n.LastHeartbeat, &n.LastSequence, &caps, &hb, &n.CreatedAt, &n.AllowSubscription, &n.Maintenance, &n.DeletedAt, &n.PublicIPManual, &n.ConnectHost, &addrsJSON, &n.PublicIPSelected, &n.AgentUpdateSupported, &fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, "", expires, ErrNotFound
	}
	if err != nil {
		return Node{}, "", expires, err
	}
	if addrsJSON != "" && addrsJSON != "[]" {
		_ = json.Unmarshal([]byte(addrsJSON), &n.PublicAddresses)
	}
	n.PublicIPSource = publicIPSource(n.PublicIP, n.PublicIPSelected)
	if n.DeletedAt != "" || !n.Enabled || fingerprint == "" {
		return Node{}, "", expires, ErrNodeNotEnrolled
	}
	blockers, err := recoveryBlockers(ctx, tx, nodeID)
	if err != nil {
		return Node{}, "", expires, err
	}
	if len(blockers) > 0 {
		return Node{}, "", expires, &BlockedError{Items: blockers, Code: "node_recovery_blocked"}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE node_enrollment_tokens SET consumed_at=? WHERE node_id=? AND purpose='recover' AND consumed_at IS NULL", timestamp(now), nodeID); err != nil {
		return Node{}, "", expires, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE node_identity_recoveries SET status='superseded' WHERE node_id=? AND status='issued'", nodeID); err != nil {
		return Node{}, "", expires, err
	}
	revoked := 0
	if revokeNow {
		revoked = 1
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO node_enrollment_tokens(token_hash,node_id,expires_at,purpose,bound_fingerprint) VALUES(?,?,?,'recover',?)", Hash(token), nodeID, timestamp(expires), fingerprint); err != nil {
		return Node{}, "", expires, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO node_identity_recoveries(id,node_id,token_hash,previous_fingerprint,owner,revoked_on_issue,status,created_at,expires_at) VALUES(?,?,?,?,?,?,'issued',?,?)", recoveryID, nodeID, Hash(token), fingerprint, owner, revoked, timestamp(now), timestamp(expires)); err != nil {
		return Node{}, "", expires, err
	}
	if revokeNow {
		if _, err = tx.ExecContext(ctx, "UPDATE node_identities SET revoked_at=? WHERE node_id=?", timestamp(now), nodeID); err != nil {
			return Node{}, "", expires, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM node_certificate_renewals WHERE node_id=?", nodeID); err != nil {
			return Node{}, "", expires, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Node{}, "", expires, err
	}
	return n, token, expires, nil
}

func (r *Repository) consumeRecoveryTx(ctx context.Context, tx *sql.Tx, hash, nodeID string, receipt *enrollmentReceipt) error {
	now := time.Now().UTC()
	consumedAt := timestamp(now)
	if receipt != nil && receipt.CreatedAt != "" {
		consumedAt = receipt.CreatedAt
	}
	result, err := tx.ExecContext(ctx, "UPDATE node_enrollment_tokens SET consumed_at=? WHERE token_hash=? AND node_id=? AND purpose='recover' AND consumed_at IS NULL AND expires_at>? AND EXISTS(SELECT 1 FROM nodes WHERE nodes.id=node_id AND enabled=1 AND deleted_at IS NULL)", consumedAt, hash, nodeID, timestamp(now))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrToken
	}
	var bound string
	if err := tx.QueryRowContext(ctx, "SELECT bound_fingerprint FROM node_enrollment_tokens WHERE token_hash=?", hash).Scan(&bound); err != nil {
		return ErrToken
	}
	var current string
	if err := tx.QueryRowContext(ctx, "SELECT fingerprint FROM node_identities WHERE node_id=?", nodeID).Scan(&current); err != nil {
		return ErrToken
	}
	if current != bound {
		return ErrToken
	}
	blockers, err := recoveryBlockers(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	if len(blockers) > 0 {
		return &BlockedError{Items: blockers, Code: "node_recovery_blocked"}
	}
	var recID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM node_identity_recoveries WHERE token_hash=? AND status='issued'", hash).Scan(&recID); err != nil {
		return ErrToken
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO node_identity_retirements(fingerprint,node_id,recovery_id,retired_at) VALUES(?,?,?,?)", current, nodeID, recID, timestamp(now)); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, "UPDATE node_identities SET certificate_pem=?,fingerprint=?,expires_at=?,revoked_at=NULL WHERE node_id=? AND fingerprint=?", receipt.CertificatePEM, receipt.Fingerprint, receipt.ExpiresAt, nodeID, current)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return ErrToken
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM node_certificate_renewals WHERE node_id=?", nodeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM node_enrollment_receipts WHERE node_id=?", nodeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO node_enrollment_receipts("+enrollmentReceiptColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", receipt.TokenHash, receipt.NodeID, receipt.RequestDigest, receipt.PublicKeyDigest, receipt.Hostname, receipt.OS, receipt.AgentVersion, receipt.CertificatePEM, receipt.Fingerprint, receipt.CAPEM, receipt.ExpiresAt, receipt.CreatedAt); err != nil {
		return err
	}
	for _, table := range []string{"node_health_checks", "node_tcp_probes"} {
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET status='expired' WHERE node_id=? AND status='queued'", nodeID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET status='offline',heartbeat_json='{}',tcp_probe_supported=0,multi_core_supported=0,node_traffic_supported=0,gost_core_supported=0,core_restart_supported=0,hostname=?,operating_system=?,agent_version=? WHERE id=?", receipt.Hostname, receipt.OS, receipt.AgentVersion, nodeID); err != nil {
		return err
	}
	runtime := 0
	_ = tx.QueryRowContext(ctx, "SELECT count(*) FROM node_runtime_state WHERE node_id=?", nodeID).Scan(&runtime)
	state := "not_needed"
	if runtime > 0 {
		state = "pending"
	}
	if _, err := tx.ExecContext(ctx, "UPDATE node_identity_recoveries SET status='consumed',new_fingerprint=?,consumed_at=?,redeploy_state=? WHERE id=?", receipt.Fingerprint, timestamp(now), state, recID); err != nil {
		return err
	}
	return nil
}

func (r *Repository) RecoveryStatus(ctx context.Context, nodeID string) (Recovery, error) {
	var x Recovery
	err := r.Database.QueryRowContext(ctx, "SELECT id,node_id,previous_fingerprint,new_fingerprint,owner,revoked_on_issue,status,redeploy_state,redeploy_revision,redeploy_code,created_at,expires_at,consumed_at FROM node_identity_recoveries WHERE node_id=? ORDER BY created_at DESC LIMIT 1", nodeID).Scan(&x.ID, &x.NodeID, &x.PreviousFingerprint, &x.NewFingerprint, &x.Owner, &x.RevokedOnIssue, &x.Status, &x.RedeployState, &x.RedeployRevision, &x.RedeployCode, &x.CreatedAt, &x.ExpiresAt, &x.ConsumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	if err != nil {
		return x, err
	}
	// A read never writes: the deadline is displayed as expired, while the
	// stored row still tells the enrollment path that the code went unused.
	if x.Status == "issued" {
		if deadline, parseErr := time.Parse(time.RFC3339Nano, x.ExpiresAt); parseErr == nil && !time.Now().UTC().Before(deadline) {
			x.Status = "expired"
		}
	}
	return x, nil
}

// ClaimRecoveryRedeploy takes the node's pending redeploy. A claim that never
// reached a revision (the panel stopped between the claim and the deploy) is
// claimable again, so the next capability report retries instead of leaving the
// row queued forever.
func (r *Repository) ClaimRecoveryRedeploy(ctx context.Context, nodeID string) (bool, error) {
	res, err := r.Database.ExecContext(ctx, "UPDATE node_identity_recoveries SET redeploy_state='queued' WHERE node_id=? AND (redeploy_state='pending' OR redeploy_state='queued' AND redeploy_revision=0)", nodeID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
func (r *Repository) FinishRecoveryRedeploy(ctx context.Context, nodeID string, revision int64, state, code string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE node_identity_recoveries SET redeploy_state=?,redeploy_revision=?,redeploy_code=? WHERE node_id=? AND redeploy_state IN ('queued','pending')", state, revision, code, nodeID)
	return err
}

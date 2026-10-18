package node

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"proxycontrol/internal/platform/pki"
)

type EnrollmentStatus struct {
	NodeID                       string                 `json:"nodeId"`
	ObservedAt                   string                 `json:"observedAt"`
	Code                         string                 `json:"code"`
	Summary                      string                 `json:"summary"`
	ReceiptState                 string                 `json:"receiptState"`
	OriginalFingerprint          string                 `json:"originalFingerprint,omitempty"`
	OriginalCreatedAt            string                 `json:"originalCreatedAt,omitempty"`
	OriginalExpiresAt            string                 `json:"originalExpiresAt,omitempty"`
	CurrentCertificate           CertificateObservation `json:"currentCertificate"`
	CurrentIdentityMatches       bool                   `json:"currentIdentityMatches"`
	CurrentCertificateValid      bool                   `json:"currentCertificateValid"`
	Enabled                      bool                   `json:"enabled"`
	IdentityRevoked              bool                   `json:"identityRevoked"`
	LastSequence                 uint64                 `json:"lastSequence"`
	PendingRenewal               bool                   `json:"pendingRenewal"`
	TokenState                   string                 `json:"tokenState"`
	TokenExpiresAt               string                 `json:"tokenExpiresAt,omitempty"`
	CentralReplayConditionsMatch bool                   `json:"centralReplayConditionsMatch"`
	LocalInstallationConfirmed   bool                   `json:"localInstallationConfirmed"`
	StateRecoveryConfirmed       bool                   `json:"stateRecoveryConfirmed"`
	NextSteps                    []string               `json:"nextSteps"`
}

func readEnrollmentStatusTx(ctx context.Context, tx *sql.Tx, nodeID string, item *enrollmentReceipt, authority *pki.Authority, at time.Time) (EnrollmentStatus, error) {
	result := EnrollmentStatus{NodeID: nodeID, ObservedAt: timestamp(at), ReceiptState: "missing", TokenState: "unknown", Code: "original_receipt_missing", Summary: "没有原登记回执；不能从当前身份补造原签发结果。", NextSteps: []string{"本地材料安装须使用原登记结果及停止节点诊断核对。", "消费、额度、并发、消息及更新缺口仍分别保留；本页不会登记、签发、清理或结算。"}}
	var certificate, fingerprint, expiry string
	var revoked sql.NullString
	var enabled, sequence int64
	err := tx.QueryRowContext(ctx, "SELECT coalesce(i.certificate_pem,''),coalesce(i.fingerprint,''),coalesce(i.expires_at,''),i.revoked_at,CASE WHEN typeof(n.enabled)='integer' THEN n.enabled ELSE -1 END,CASE WHEN typeof(n.last_sequence)='integer' THEN n.last_sequence ELSE -1 END FROM nodes n LEFT JOIN node_identities i ON i.node_id=n.id WHERE n.id=?", nodeID).Scan(&certificate, &fingerprint, &expiry, &revoked, &enabled, &sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if (enabled != 0 && enabled != 1) || sequence < 0 {
		return result, ErrSequence
	}
	result.Enabled = enabled == 1
	result.IdentityRevoked = revoked.Valid
	result.LastSequence = uint64(sequence)
	observation, leaf := certificateMetadata(nodeID, certificate, fingerprint, expiry, at)
	observation.Issuer = certificateIssuerRelation(authority, leaf)
	if revoked.Valid {
		observation.Status = "revoked"
		observation.RevokedAt = revoked.String
	}
	result.CurrentCertificate = observation
	result.CurrentCertificateValid = !revoked.Valid && centralCertificateValid(authority, leaf, at)
	var pending int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM node_certificate_renewals WHERE node_id=?", nodeID).Scan(&pending); err != nil {
		return result, err
	}
	result.PendingRenewal = pending != 0
	if item == nil {
		return result, nil
	}
	result.ReceiptState = "verified"
	result.OriginalFingerprint = item.Fingerprint
	result.OriginalCreatedAt = item.CreatedAt
	result.OriginalExpiresAt = item.ExpiresAt
	result.CurrentIdentityMatches = certificate == item.CertificatePEM && fingerprint == item.Fingerprint && expiry == item.ExpiresAt
	var tokenNode, tokenExpiry string
	var consumed sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT node_id,expires_at,consumed_at FROM node_enrollment_tokens WHERE token_hash=?", item.TokenHash).Scan(&tokenNode, &tokenExpiry, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		result.TokenState = "missing"
	} else if err != nil {
		return result, err
	} else {
		deadline, e := time.Parse(time.RFC3339Nano, tokenExpiry)
		switch {
		case tokenNode != nodeID || !consumed.Valid || consumed.String != item.CreatedAt:
			result.TokenState = "original_consumption_conflict"
		case e != nil:
			result.TokenState = "invalid_expiry"
		case !at.Before(deadline):
			result.TokenState = "expired"
			result.TokenExpiresAt = timestamp(deadline)
		default:
			result.TokenState = "consumed_window_current"
			result.TokenExpiresAt = timestamp(deadline)
		}
	}
	result.CentralReplayConditionsMatch = result.TokenState == "consumed_window_current" && result.Enabled && !result.IdentityRevoked && result.LastSequence == 0 && !result.PendingRenewal && result.CurrentIdentityMatches && result.CurrentCertificateValid
	result.Code = "original_receipt_retained_requires_review"
	result.Summary = "原登记回执可核对；当前条件不支持重放原登记结果，历史回执继续保留。"
	if result.CentralReplayConditionsMatch {
		result.Code = "central_replay_conditions_match"
		result.Summary = "中央原回执、身份和窗口条件吻合；仍需同一私钥有效CSR及原请求信息，不代表本地已安装。"
	}
	if result.LastSequence > 0 {
		result.NextSteps = append(result.NextSteps, "节点已有确认消息，不能以初始登记接续覆盖当前业务状态；本页不判断节点当前在线。")
	}
	return result, nil
}

func (r *Repository) EnrollmentStatus(ctx context.Context, nodeID string, authority *pki.Authority) (EnrollmentStatus, error) {
	var result EnrollmentStatus
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	item, err := scanEnrollmentReceipt(tx.QueryRowContext(ctx, "SELECT "+enrollmentReceiptColumns+" FROM node_enrollment_receipts WHERE node_id=?", nodeID))
	var original *enrollmentReceipt
	if err == nil {
		original = &item
	} else if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	result, err = readEnrollmentStatusTx(ctx, tx, nodeID, original, authority, time.Now().UTC())
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

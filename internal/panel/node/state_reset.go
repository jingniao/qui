package node

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

// ResetLegacyState deliberately never requests telemetry drain. The old peer
// can replay only the result bound to its reset ID, new key and release digest.
func (c *Control) ResetLegacyState(ctx context.Context, request *pb.LegacyStateResetRequest) (*pb.EnrollResponse, error) {
	if request == nil || !replacementID(request.ResetId) || !recoveryDigest(request.TargetSha256, false) || !release.SupportedAgentVersion(request.AgentVersion) {
		return nil, status.Error(codes.InvalidArgument, "invalid legacy state reset")
	}
	info, err := certificate(ctx)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "node certificate required")
	}
	old := info.State.PeerCertificates[0]
	nodeID, err := pki.NodeID(old)
	if err != nil {
		return nil, err
	}
	csr, err := pki.ParseCSR(request.CsrPem)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid reset CSR")
	}
	public := Hash(string(csr.RawSubjectPublicKeyInfo))
	fingerprint := pki.Fingerprint(old)
	tx, err := c.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var savedNode, previous, savedPublic, target, version, expiry, savedFingerprint, redeploy string
	result := &pb.EnrollResponse{}
	err = tx.QueryRowContext(ctx, "SELECT node_id,previous_fingerprint,public_key_digest,target_sha256,agent_version,certificate_pem,ca_pem,expires_at,fingerprint FROM agent_state_resets WHERE reset_id=?", request.ResetId).Scan(&savedNode, &previous, &savedPublic, &target, &version, &result.CertificatePem, &result.CaPem, &expiry, &savedFingerprint)
	if err == nil {
		if savedNode != nodeID || savedPublic != public || target != request.TargetSha256 || version != request.AgentVersion || fingerprint != previous && fingerprint != savedFingerprint {
			return nil, status.Error(codes.PermissionDenied, "reset binding conflicts with original")
		}
		deadline, err := time.Parse(time.RFC3339Nano, expiry)
		if err != nil {
			return nil, err
		}
		_ = tx.QueryRowContext(ctx, "SELECT redeploy_state FROM node_identity_recoveries WHERE id=?", request.ResetId).Scan(&redeploy)
		result.NodeId = nodeID
		result.StateResetStatus = redeploy
		result.ExpiresAt = timestamppb.New(deadline)
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var valid int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM node_identities i JOIN nodes n ON n.id=i.node_id WHERE i.node_id=? AND i.fingerprint=? AND i.revoked_at IS NULL AND n.enabled=1 AND n.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM agent_state_resets r WHERE r.node_id=i.node_id) AND NOT EXISTS(SELECT 1 FROM node_identity_retirements r WHERE r.fingerprint=i.fingerprint)", nodeID, fingerprint).Scan(&valid); err != nil {
		return nil, err
	}
	if valid != 1 || public == Hash(string(old.RawSubjectPublicKeyInfo)) {
		return nil, status.Error(codes.PermissionDenied, "legacy reset requires current identity and a new key")
	}
	encoded, leaf, err := c.Authority.IssueNode(csr, nodeID)
	if err != nil {
		return nil, err
	}
	now := timestamp(time.Now())
	expiry = timestamp(leaf.NotAfter)
	if _, err = tx.ExecContext(ctx, "INSERT INTO agent_state_resets(reset_id,node_id,previous_fingerprint,public_key_digest,target_sha256,agent_version,certificate_pem,ca_pem,fingerprint,expires_at,sealed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", request.ResetId, nodeID, fingerprint, public, request.TargetSha256, request.AgentVersion, encoded, c.Authority.PEM, pki.Fingerprint(leaf), expiry, now); err != nil {
		return nil, err
	}
	// Reuse the existing published-state redeploy lifecycle. No recovery token
	// exists for this row and its digest cannot authorize an enrollment.
	if _, err = tx.ExecContext(ctx, "INSERT INTO node_identity_recoveries(id,node_id,token_hash,previous_fingerprint,new_fingerprint,owner,revoked_on_issue,status,redeploy_state,created_at,expires_at,consumed_at) VALUES(?,?,?,?,?,'legacy-state-reset',1,'consumed','pending',?,?,?)", request.ResetId, nodeID, Hash("legacy-state-reset:"+request.ResetId), fingerprint, pki.Fingerprint(leaf), now, expiry, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO node_identity_retirements(fingerprint,node_id,recovery_id,retired_at) VALUES(?,?,?,?)", fingerprint, nodeID, request.ResetId, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE node_identities SET certificate_pem=?,fingerprint=?,expires_at=?,revoked_at=NULL WHERE node_id=?", string(encoded), pki.Fingerprint(leaf), expiry, nodeID); err != nil {
		return nil, err
	}
	for _, table := range []string{"access_batches", "traffic_batches", "traffic_pending_batches"} {
		if _, err = tx.ExecContext(ctx, "UPDATE "+table+" SET reset_id=? WHERE node_id=? AND reset_id=''", request.ResetId, nodeID); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE traffic_delivery_queues SET reset_id=?,closed_at=?,recovery_id=?,updated_at=? WHERE node_id=? AND reset_id=''", request.ResetId, now, request.ResetId, now, nodeID); err != nil {
		return nil, err
	}
	for _, table := range []string{"node_certificate_renewals", "node_enrollment_receipts"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE node_id=?", nodeID); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE node_enrollment_tokens SET consumed_at=? WHERE node_id=? AND consumed_at IS NULL", now, nodeID); err != nil {
		return nil, err
	}
	for _, table := range []string{"node_health_checks", "node_tcp_probes"} {
		if _, err = tx.ExecContext(ctx, "UPDATE "+table+" SET status='expired' WHERE node_id=? AND status='queued'", nodeID); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE nodes SET status='offline',heartbeat_json='{}',agent_version=? WHERE id=?", request.AgentVersion, nodeID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	c.Disconnect(nodeID)
	c.Runtime.afterCommit(nodeID)
	return &pb.EnrollResponse{NodeId: nodeID, CertificatePem: encoded, CaPem: c.Authority.PEM, ExpiresAt: timestamppb.New(leaf.NotAfter), StateResetStatus: "pending"}, nil
}

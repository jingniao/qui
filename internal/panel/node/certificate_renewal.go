package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

// Renewal keeps the independent node key. New certificate possession on the
// next mTLS connection confirms activation; old valid identity remains usable
// until that confirmation, including retry after a lost renewal response.
func (c *Control) RenewCertificate(ctx context.Context, request *pb.CertificateRequest) (*pb.CertificateRenewal, error) {
	info, err := certificate(ctx)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "node certificate required")
	}
	leaf := info.State.PeerCertificates[0]
	nodeID, err := pki.NodeID(leaf)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid node identity")
	}
	fingerprint := pki.Fingerprint(leaf)
	if err := c.Repository.ValidateIdentity(ctx, nodeID, fingerprint); err != nil {
		return nil, status.Error(codes.Unauthenticated, "node certificate revoked or disabled")
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "signed CSR required")
	}
	if request.ConfirmOnly {
		if request.Immediate || request.RotateKey || len(request.CsrPem) != 0 {
			return nil, status.Error(codes.InvalidArgument, "confirmation cannot request issuance")
		}
		issuer, err := c.Authority.CertificateIssuerPEM(leaf)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, "node certificate issuer is unavailable")
		}
		return &pb.CertificateRenewal{CertificatePem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw}), CaPem: issuer, ExpiresAt: timestamppb.New(leaf.NotAfter), ExplicitRenewalSupported: true, KeyRotationSupported: true, Confirmed: true}, nil
	}
	csr, err := pki.ParseCSR(request.CsrPem)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid signed ECDSA CSR")
	}
	oldKey, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	newKey, newOK := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || !newOK || (!request.RotateKey && !oldKey.Equal(newKey)) || (request.RotateKey && (oldKey.Equal(newKey) || !request.Immediate)) {
		return nil, status.Error(codes.InvalidArgument, "renewal must retain authenticated node key")
	}
	if !request.Immediate && time.Until(leaf.NotAfter) > 30*24*time.Hour {
		return nil, status.Error(codes.FailedPrecondition, "node certificate renewal is not due")
	}
	encoded, renewed, err := c.Authority.IssueNode(csr, nodeID)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "node CA cannot issue this certificate")
	}
	if !renewed.NotAfter.After(leaf.NotAfter) {
		return nil, status.Error(codes.FailedPrecondition, "node CA cannot extend this certificate")
	}
	result, err := c.Repository.StageRenewal(ctx, nodeID, fingerprint, string(encoded), renewed)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "node renewal could not be persisted")
	}
	result.CaPem = c.Authority.PEM
	result.ExplicitRenewalSupported = true
	result.KeyRotationSupported = true
	block, _ := pem.Decode(result.CertificatePem)
	if block == nil {
		return nil, status.Error(codes.FailedPrecondition, "pending certificate is invalid")
	}
	pending, parseErr := x509.ParseCertificate(block.Bytes)
	if parseErr != nil {
		return nil, status.Error(codes.FailedPrecondition, "pending certificate is invalid")
	}
	pendingKey, keyOK := pending.PublicKey.(*ecdsa.PublicKey)
	if !keyOK || !pendingKey.Equal(newKey) {
		return nil, status.Error(codes.FailedPrecondition, "another certificate request is pending")
	}
	result.CaPem, err = c.Authority.CertificateIssuerPEM(pending)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "pending certificate issuer is unavailable")
	}
	return result, nil
}

// StageRenewal persists trusted issuer output; callers authenticate the peer and
// verify CSR key possession before invoking this module-owned repository step.
func (r *Repository) StageRenewal(ctx context.Context, nodeID, previous, encoded string, certificate *x509.Certificate) (*pb.CertificateRenewal, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var active string
	var enabled bool
	if err := tx.QueryRowContext(ctx, "SELECT i.fingerprint,n.enabled FROM node_identities i JOIN nodes n ON n.id=i.node_id WHERE i.node_id=? AND i.revoked_at IS NULL AND i.expires_at>?", nodeID, timestamp(time.Now())).Scan(&active, &enabled); err != nil || !enabled || active != previous {
		return nil, ErrIdentity
	}
	var pending, old, expiry string
	err = tx.QueryRowContext(ctx, "SELECT certificate_pem,previous_fingerprint,expires_at FROM node_certificate_renewals WHERE node_id=?", nodeID).Scan(&pending, &old, &expiry)
	if err == nil {
		if old != previous {
			return nil, ErrIdentity
		}
		expires, err := time.Parse(time.RFC3339Nano, expiry)
		if err != nil || !time.Now().Before(expires) {
			return nil, ErrIdentity
		}
		return &pb.CertificateRenewal{CertificatePem: []byte(pending), ExpiresAt: timestamppb.New(expires)}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO node_certificate_renewals(node_id,previous_fingerprint,certificate_pem,fingerprint,expires_at,created_at) VALUES(?,?,?,?,?,?)", nodeID, previous, encoded, pki.Fingerprint(certificate), timestamp(certificate.NotAfter), timestamp(time.Now())); err != nil {
		return nil, err
	}
	if r.CertificateAudit != nil {
		if err := r.CertificateAudit.RecordNodeCertificate(ctx, tx, nodeID, "签发节点续期证书"); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &pb.CertificateRenewal{CertificatePem: []byte(encoded), ExpiresAt: timestamppb.New(certificate.NotAfter)}, nil
}

func (r *Repository) activateRenewal(ctx context.Context, nodeID, fingerprint string) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var encoded, expiry, previous string
	err = tx.QueryRowContext(ctx, `SELECT p.certificate_pem,p.expires_at,p.previous_fingerprint FROM node_certificate_renewals p JOIN node_identities i ON i.node_id=p.node_id JOIN nodes n ON n.id=p.node_id
WHERE p.node_id=? AND p.fingerprint=? AND p.previous_fingerprint=i.fingerprint AND i.revoked_at IS NULL AND n.enabled=1 AND p.expires_at>?`, nodeID, fingerprint, timestamp(time.Now())).Scan(&encoded, &expiry, &previous)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrIdentity
	}
	if err != nil {
		return err
	}
	block, rest := pem.Decode([]byte(encoded))
	if block == nil || len(rest) != 0 || block.Type != "CERTIFICATE" {
		return ErrIdentity
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil || pki.Fingerprint(leaf) != fingerprint || timestamp(leaf.NotAfter) != expiry {
		return ErrIdentity
	}
	identityID, err := pki.NodeID(leaf)
	if err != nil || identityID != nodeID {
		return ErrIdentity
	}
	updated, err := tx.ExecContext(ctx, "UPDATE node_identities SET certificate_pem=?,fingerprint=?,expires_at=? WHERE node_id=? AND fingerprint=? AND revoked_at IS NULL", encoded, fingerprint, expiry, nodeID, previous)
	if err != nil {
		return err
	}
	if count, err := updated.RowsAffected(); err != nil || count != 1 {
		return ErrIdentity
	}
	removed, err := tx.ExecContext(ctx, "DELETE FROM node_certificate_renewals WHERE node_id=? AND fingerprint=? AND previous_fingerprint=?", nodeID, fingerprint, previous)
	if err != nil {
		return err
	}
	if count, err := removed.RowsAffected(); err != nil || count != 1 {
		return ErrIdentity
	}
	if r.CertificateAudit != nil {
		if err := r.CertificateAudit.RecordNodeCertificate(ctx, tx, nodeID, "确认节点续期证书"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

package node

import (
	"bytes"
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
)

type enrollmentReceipt struct {
	Hostname, OS, AgentVersion                               string
	TokenHash, NodeID, RequestDigest, PublicKeyDigest        string
	CertificatePEM, Fingerprint, CAPEM, ExpiresAt, CreatedAt string
}

const enrollmentReceiptColumns = "token_hash,node_id,request_digest,public_key_digest,hostname,operating_system,agent_version,certificate_pem,certificate_fingerprint,ca_pem,expires_at,created_at"

// A CSR signature can change on retry. Bind its verified public key and fixed
// machine metadata, never its random signature bytes or the plaintext token.
func enrollmentRequestDigest(request *pb.EnrollRequest, csr *x509.CertificateRequest) (string, string, error) {
	if request == nil || csr == nil || len(request.EnrollmentToken) == 0 || len(request.EnrollmentToken) > 128 || len(request.Hostname) > 128 || len(request.OperatingSystem) > 256 || len(request.AgentVersion) > 64 {
		return "", "", ErrToken
	}
	public := Hash(string(csr.RawSubjectPublicKeyInfo))
	data, err := json.Marshal(struct{ PublicKey, Hostname, OS, Version string }{public, request.Hostname, request.OperatingSystem, request.AgentVersion})
	if err != nil {
		return "", "", err
	}
	return Hash(string(data)), public, nil
}
func scanEnrollmentReceipt(row interface{ Scan(...any) error }) (enrollmentReceipt, error) {
	var item enrollmentReceipt
	err := row.Scan(&item.TokenHash, &item.NodeID, &item.RequestDigest, &item.PublicKeyDigest, &item.Hostname, &item.OS, &item.AgentVersion, &item.CertificatePEM, &item.Fingerprint, &item.CAPEM, &item.ExpiresAt, &item.CreatedAt)
	if err != nil {
		return item, err
	}
	for _, digest := range []string{item.TokenHash, item.RequestDigest, item.PublicKeyDigest, item.Fingerprint} {
		if !recoveryDigest(digest, false) {
			return item, ErrToken
		}
	}
	if len(item.Hostname) > 128 || len(item.OS) > 256 || len(item.AgentVersion) > 64 {
		return item, ErrToken
	}
	metadata, err := json.Marshal(struct{ PublicKey, Hostname, OS, Version string }{item.PublicKeyDigest, item.Hostname, item.OS, item.AgentVersion})
	if err != nil || Hash(string(metadata)) != item.RequestDigest {
		return item, ErrToken
	}
	if !replacementID(item.NodeID) || len(item.CertificatePEM) == 0 || len(item.CertificatePEM) > 32768 || len(item.CAPEM) == 0 || len(item.CAPEM) > 32768 {
		return item, ErrToken
	}
	created, e := time.Parse(time.RFC3339Nano, item.CreatedAt)
	if e != nil {
		return item, ErrToken
	}
	expiry, e := time.Parse(time.RFC3339Nano, item.ExpiresAt)
	if e != nil || !expiry.After(created) {
		return item, ErrToken
	}
	_, leaf := certificateMetadata(item.NodeID, item.CertificatePEM, item.Fingerprint, item.ExpiresAt, created)
	if leaf == nil || Hash(string(leaf.RawSubjectPublicKeyInfo)) != item.PublicKeyDigest {
		return item, ErrToken
	}
	roots := x509.NewCertPool()
	anchor, rest := pem.Decode([]byte(item.CAPEM))
	if anchor == nil || anchor.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return item, ErrToken
	}
	ca, err := x509.ParseCertificate(anchor.Bytes)
	if err != nil || !ca.IsCA || ca.KeyUsage&x509.KeyUsageCertSign == 0 || created.Before(ca.NotBefore) || !created.Before(ca.NotAfter) {
		return item, ErrToken
	}
	if !roots.AppendCertsFromPEM([]byte(item.CAPEM)) {
		return item, ErrToken
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: created, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return item, ErrToken
	}
	return item, nil
}

func (r *Repository) ReplayEnrollment(ctx context.Context, request *pb.EnrollRequest, csr *x509.CertificateRequest, authority *pki.Authority) (*pb.EnrollResponse, bool, error) {
	digest, public, err := enrollmentRequestDigest(request, csr)
	if err != nil {
		return nil, false, err
	}
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	item, err := scanEnrollmentReceipt(tx.QueryRowContext(ctx, "SELECT "+enrollmentReceiptColumns+" FROM node_enrollment_receipts WHERE token_hash=?", Hash(request.EnrollmentToken)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	if item.RequestDigest != digest || item.PublicKeyDigest != public {
		return nil, true, ErrToken
	}
	at := time.Now().UTC()
	facts, err := readEnrollmentStatusTx(ctx, tx, item.NodeID, &item, authority, at)
	if err != nil {
		return nil, true, err
	}
	if !facts.CentralReplayConditionsMatch {
		return nil, true, ErrToken
	}
	_, leaf := certificateMetadata(item.NodeID, item.CertificatePEM, item.Fingerprint, item.ExpiresAt, at)
	if leaf == nil || !bytes.Equal(leaf.RawSubjectPublicKeyInfo, csr.RawSubjectPublicKeyInfo) {
		return nil, true, ErrToken
	}
	if err := tx.Commit(); err != nil {
		return nil, true, err
	}
	return &pb.EnrollResponse{NodeId: item.NodeID, CertificatePem: []byte(item.CertificatePEM), CaPem: []byte(item.CAPEM), ExpiresAt: timestamppb.New(leaf.NotAfter)}, true, nil
}

func (r *Repository) ConsumeEnrollmentOriginal(ctx context.Context, hash, id string, request *pb.EnrollRequest, csr *x509.CertificateRequest, certificate, ca []byte, leaf *x509.Certificate) error {
	digest, public, err := enrollmentRequestDigest(request, csr)
	if err != nil {
		return err
	}
	if leaf == nil || Hash(request.EnrollmentToken) != hash || !bytes.Equal(leaf.RawSubjectPublicKeyInfo, csr.RawSubjectPublicKeyInfo) {
		return ErrToken
	}
	item := enrollmentReceipt{TokenHash: hash, NodeID: id, RequestDigest: digest, PublicKeyDigest: public, Hostname: request.Hostname, OS: request.OperatingSystem, AgentVersion: request.AgentVersion, CertificatePEM: string(certificate), Fingerprint: pki.Fingerprint(leaf), CAPEM: string(ca), ExpiresAt: timestamp(leaf.NotAfter), CreatedAt: timestamp(time.Now().UTC())}
	if len(certificate) > 32768 || len(ca) > 32768 || !replacementID(id) || csr.CheckSignature() != nil {
		return ErrToken
	}
	return r.consumeEnrollment(ctx, hash, id, request.Hostname, request.OperatingSystem, request.AgentVersion, item.CertificatePEM, item.Fingerprint, leaf.NotAfter, &item)
}

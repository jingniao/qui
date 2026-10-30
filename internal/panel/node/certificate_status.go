package node

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/internal/platform/pki"
	"time"
)

type CertificateObservation struct {
	Issuer      string `json:"issuer"`
	Fingerprint string `json:"fingerprint,omitempty"`
	NotBefore   string `json:"notBefore,omitempty"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
	Status      string `json:"status"`
	Integrity   string `json:"integrity"`
	RevokedAt   string `json:"revokedAt,omitempty"`
}
type PendingCertificateObservation struct {
	CertificateObservation
	CreatedAt     string `json:"createdAt"`
	Purpose       string `json:"purpose"`
	SourceMatches bool   `json:"sourceMatches"`
}
type CertificateStatus struct {
	IdentitySnapshotDigest  string                         `json:"identitySnapshotDigest"`
	SigningCAFingerprint    string                         `json:"signingCAFingerprint,omitempty"`
	CurrentSigningAvailable bool                           `json:"currentSigningAvailable"`
	NodeID                  string                         `json:"nodeId"`
	Enabled                 bool                           `json:"enabled"`
	ObservedAt              string                         `json:"observedAt"`
	Active                  *CertificateObservation        `json:"active"`
	Pending                 *PendingCertificateObservation `json:"pending"`
}

func certificateMetadata(nodeID, encoded, fingerprint, expiry string, at time.Time) (CertificateObservation, *x509.Certificate) {
	result := CertificateObservation{Status: "unknown", Integrity: "invalid"}
	if len(encoded) > 1<<20 {
		return result, nil
	}
	block, rest := pem.Decode([]byte(encoded))
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return result, nil
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return result, nil
	}
	id, err := pki.NodeID(leaf)
	if err != nil || id != nodeID || pki.Fingerprint(leaf) != fingerprint {
		return result, nil
	}
	storedExpiry, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || !storedExpiry.Equal(leaf.NotAfter) {
		return result, nil
	}
	key, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() || leaf.IsCA {
		return result, nil
	}
	clientAuth := false
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			clientAuth = true
		}
	}
	if !clientAuth {
		return result, nil
	}
	result.Fingerprint = fingerprint
	result.NotBefore = timestamp(leaf.NotBefore)
	result.ExpiresAt = timestamp(leaf.NotAfter)
	result.Integrity = "consistent"
	switch {
	case at.Before(leaf.NotBefore):
		result.Status = "not_yet_valid"
	case !at.Before(leaf.NotAfter):
		result.Status = "expired"
	case certificateRenewalDue(leaf, at):
		result.Status = "renewal_due"
	default:
		result.Status = "valid"
	}
	return result, leaf
}

// Return lifecycle metadata only; this observation does not authorize identity
// activation or replace mTLS certificate/issuer verification.
func (r *Repository) CertificateStatus(ctx context.Context, nodeID string, authorities ...*pki.Authority) (CertificateStatus, error) {
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CertificateStatus{}, err
	}
	defer tx.Rollback()
	result := CertificateStatus{NodeID: nodeID}
	var authority *pki.Authority
	if len(authorities) > 0 {
		authority = authorities[0]
	}
	if authority != nil {
		result.SigningCAFingerprint = pki.Fingerprint(authority.Certificate)
		result.CurrentSigningAvailable = authority.ValidateCurrentSigning() == nil
	}
	issuer := func(leaf *x509.Certificate) string { return certificateIssuerRelation(authority, leaf) }
	var activePEM, activeFingerprint, activeExpiry, revoked, pendingPEM, pendingFingerprint, pendingExpiry, previous, created sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT n.enabled,i.certificate_pem,i.fingerprint,i.expires_at,i.revoked_at,p.certificate_pem,p.fingerprint,p.expires_at,p.previous_fingerprint,p.created_at FROM nodes n LEFT JOIN node_identities i ON i.node_id=n.id LEFT JOIN node_certificate_renewals p ON p.node_id=n.id WHERE n.id=?`, nodeID).Scan(&result.Enabled, &activePEM, &activeFingerprint, &activeExpiry, &revoked, &pendingPEM, &pendingFingerprint, &pendingExpiry, &previous, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	revocationSnapshot := ""
	if revoked.Valid {
		revocationSnapshot = "present:" + revoked.String
	}
	result.IdentitySnapshotDigest = identityRecoverySnapshot(nodeID, result.Enabled, activePEM.String, activeFingerprint.String, activeExpiry.String, revocationSnapshot, pendingPEM.String, pendingFingerprint.String, pendingExpiry.String, previous.String, created.String, result.SigningCAFingerprint)
	at := time.Now().UTC()
	result.ObservedAt = timestamp(at)
	var activeLeaf *x509.Certificate
	if activePEM.Valid {
		metadata, leaf := certificateMetadata(nodeID, activePEM.String, activeFingerprint.String, activeExpiry.String, at)
		metadata.Issuer = issuer(leaf)
		activeLeaf = leaf
		metadata.RevokedAt = recoveryPublicRevocation(revocationSnapshot)
		if metadata.RevokedAt == "revoked_time_unavailable" {
			metadata.RevokedAt = ""
		}
		if revoked.Valid {
			metadata.Status = "revoked"
		} else if !result.Enabled {
			metadata.Status = "disabled"
		}
		result.Active = &metadata
	}
	if pendingPEM.Valid {
		metadata, leaf := certificateMetadata(nodeID, pendingPEM.String, pendingFingerprint.String, pendingExpiry.String, at)
		metadata.Issuer = issuer(leaf)
		pending := PendingCertificateObservation{CertificateObservation: metadata, CreatedAt: created.String, Purpose: "unknown"}
		pending.SourceMatches = activeLeaf != nil && previous.String == activeFingerprint.String
		if activeLeaf != nil && leaf != nil {
			if bytes.Equal(activeLeaf.RawSubjectPublicKeyInfo, leaf.RawSubjectPublicKeyInfo) {
				pending.Purpose = "renewal"
			} else {
				pending.Purpose = "key_rotation"
			}
		}
		switch {
		case revoked.Valid:
			pending.Status = "revoked"
		case !result.Enabled:
			pending.Status = "disabled"
		case !pending.SourceMatches:
			pending.Status = "source_mismatch"
		case metadata.Status == "valid" || metadata.Status == "renewal_due":
			pending.Status = "awaiting_confirmation"
		}
		result.Pending = &pending
	}
	return result, nil
}
func (h *Handler) certificateStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	result, err := h.Repository.CertificateStatus(r.Context(), r.PathValue("id"), h.Authority)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}

func certificateRenewalDue(leaf *x509.Certificate, at time.Time) bool {
	return !at.Before(leaf.NotBefore.Add(30*24*time.Hour)) || !leaf.NotAfter.After(at.Add(14*24*time.Hour))
}

package node

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"proxycontrol/internal/platform/pki"
)

var ErrRecoveryDecisionConflict = errors.New("identity recovery decision or identity snapshot changed")

// replacementReader is the subset of a database handle used to read retained
// identity material during validation and restore.
type replacementReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// recoveryDigest validates a lowercase hex sha256 digest, optionally allowing
// an empty value. Kept because enrollment receipts and owner-expired renewal
// still validate retained material with it.
func recoveryDigest(value string, optional bool) bool {
	if optional && value == "" {
		return true
	}
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

// replacementID validates an opaque identifier used by enrollment receipts and
// owner-expired renewal requests.
func replacementID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// identityRecoverySnapshot commits to raw central identity facts without
// exposing malformed fields or PEM data.
func identityRecoverySnapshot(nodeID string, enabled bool, certificate, fingerprint, expiry, revoked, pending, pendingFingerprint, pendingExpiry, previous, created, signer string) string {
	payload, _ := json.Marshal([]any{"node-identity-recovery-snapshot-v1", nodeID, enabled, Hash(certificate), fingerprint, expiry, revoked, Hash(pending), pendingFingerprint, pendingExpiry, previous, created, signer})
	return Hash(string(payload))
}

func recoveryPublicRevocation(value string) string {
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "present:")
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return "revoked_time_unavailable"
	}
	return value
}

func certificateIssuerRelation(authority *pki.Authority, leaf *x509.Certificate) string {
	if authority == nil || leaf == nil {
		return "unknown"
	}
	if leaf.CheckSignatureFrom(authority.Certificate) == nil {
		return "current"
	}
	return "unrecognized"
}

func centralCertificateValid(authority *pki.Authority, leaf *x509.Certificate, at time.Time) bool {
	if authority == nil || leaf == nil || !authority.IssuedByTrustedCA(leaf) {
		return false
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(authority.VerificationPEM()) {
		return false
	}
	_, err := leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: at, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	return err == nil
}

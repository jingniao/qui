package backup

import (
	"crypto/x509"
	"proxycontrol/internal/platform/pki"
)

// Archive provenance includes authenticated retired roots. This is deliberately
// separate from IssuedByTrustedCA and never authorizes a live node connection.
func authenticatesArchivedCertificate(authority *pki.Authority, initial, leaf *x509.Certificate) bool {
	if leaf == nil {
		return false
	}
	if authority == nil {
		return initial != nil && leaf.CheckSignatureFrom(initial) == nil
	}
	seen := map[*pki.Authority]bool{}
	for current := authority; current != nil && !seen[current] && len(seen) < 130; current = current.TransitionSource() {
		seen[current] = true
		if current.Certificate != nil && leaf.CheckSignatureFrom(current.Certificate) == nil {
			return true
		}
	}
	return false
}

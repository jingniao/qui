package pki

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
)

// ActiveAuthorityName is the file name of the (now unused) encrypted active
// authority record. A single node CA is stored directly as ca.crt/ca.key, so
// this record is never written or read; the name is retained only so archives
// and installations that predate the single-CA model stay byte-compatible.
const ActiveAuthorityName = "node-ca-active.enc"

var ErrTransition = errors.New("CA transition requires intact original authenticated material")

// DecodeTransitionJSON rejects duplicate keys and unknown fields in persisted
// envelopes; no attacker-controlled map overrides apply.
func DecodeTransitionJSON(data []byte, destination any) error {
	if len(data) == 0 || len(data) > 32768 {
		return ErrTransition
	}
	tokens := json.NewDecoder(bytes.NewReader(data))
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 6 {
			return ErrTransition
		}
		token, err := tokens.Token()
		if err != nil {
			return ErrTransition
		}
		if delimiter, ok := token.(json.Delim); ok {
			if delimiter != '{' {
				return ErrTransition
			}
			seen := map[string]bool{}
			for tokens.More() {
				key, err := tokens.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return ErrTransition
				}
				seen[name] = true
				if visit(depth+1) != nil {
					return ErrTransition
				}
			}
			end, err := tokens.Token()
			if err != nil || end != json.Delim('}') {
				return ErrTransition
			}
		}
		return nil
	}
	if visit(0) != nil {
		return ErrTransition
	}
	if _, err := tokens.Token(); err != io.EOF {
		return ErrTransition
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(destination) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrTransition
	}
	return nil
}

// loadActiveAuthorityMode returns the single node CA. There is no successor,
// recovered or retired authority round: the ca.crt/ca.key pair is the only
// signing authority, so any retained transition record is ignored.
func loadActiveAuthorityMode(source *Authority) (*Authority, error) {
	return source, nil
}

// VerificationPEM returns the certificate material used to verify node leaf
// certificates against the single node CA.
func (a *Authority) VerificationPEM() []byte {
	if a == nil {
		return nil
	}
	return append([]byte(nil), a.PEM...)
}

// TransitionSource returns the authority itself: a single node CA has no
// predecessor chain.
func (a *Authority) TransitionSource() *Authority { return a }

// IssuedByTrustedCA reports whether the leaf was signed by the node CA.
func (a *Authority) IssuedByTrustedCA(leaf *x509.Certificate) bool {
	return a != nil && a.Certificate != nil && leaf != nil && leaf.CheckSignatureFrom(a.Certificate) == nil
}

// CertificateIssuerPEM returns the CA certificate that signed the leaf.
func (a *Authority) CertificateIssuerPEM(leaf *x509.Certificate) ([]byte, error) {
	if a == nil || a.Certificate == nil || leaf == nil || leaf.CheckSignatureFrom(a.Certificate) != nil {
		return nil, ErrTransition
	}
	return a.PEM, nil
}

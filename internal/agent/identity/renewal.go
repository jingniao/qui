package identity

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/pki"
	"time"
)

func (s *State) RenewalDue(at time.Time) (bool, error) {
	certificate, err := s.currentCertificate()
	if err != nil {
		return false, err
	}
	return !certificate.NotAfter.After(at.Add(14 * 24 * time.Hour)), nil
}
func (s *State) CertificateFingerprint() (string, error) {
	cert, err := s.currentCertificate()
	if err != nil {
		return "", err
	}
	return pki.Fingerprint(cert), nil
}

func (s *State) currentCertificate() (*x509.Certificate, error) {
	data, key, err := s.Material()
	defer clear(key)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid local node certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

// SaveRenewal atomically replaces only the certificate, preserving the node's
// private key, CA trust, identity JSON, ACK and encrypted spool witness.
func (s *State) SaveRenewal(certificate, ca []byte) error {
	if _, err := os.Lstat(filepath.Join(s.Directory, rotationName)); err == nil {
		return errors.New("pending key rotation must be confirmed before renewal")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(certificate) > 1<<20 || len(ca) > 1<<20 {
		return errors.New("renewed certificate material exceeds limit")
	}
	trusted, err := s.TrustedNodeCA(ca)
	if err != nil {
		return err
	}
	_, private, err := s.Material()
	if err != nil {
		return err
	}
	defer clear(private)
	pair, err := tls.X509KeyPair(certificate, private)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	identifier, err := pki.NodeID(leaf)
	if err != nil || identifier != s.NodeID {
		return errors.New("renewed certificate node identity mismatch")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trusted) {
		return errors.New("invalid trusted node CA")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return err
	}
	old, err := s.currentCertificate()
	if err != nil {
		return err
	}
	if !leaf.NotAfter.After(old.NotAfter) {
		return errors.New("renewal must extend certificate validity")
	}
	return atomicWrite(filepath.Join(s.Directory, "client.crt"), certificate)
}

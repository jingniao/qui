package identity

import (
	"bytes"
	"errors"
	"path/filepath"
)

// TrustedNodeCA accepts only the single node CA written at enrollment
// (node-ca.crt). It never adopts a CA from a control response, so a control
// hint grants no new trust.
func (s *State) TrustedNodeCA(ca []byte) ([]byte, error) {
	trusted, err := s.TrustedNodeRoots()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(ca, trusted) {
		return nil, errors.New("unapproved node CA")
	}
	return trusted, nil
}

// TrustedNodeRoots returns the single node CA PEM retained at enrollment.
func (s *State) TrustedNodeRoots() ([]byte, error) {
	return readMaterialFile(filepath.Join(s.Directory, "node-ca.crt"), 16384)
}

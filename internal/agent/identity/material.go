package identity

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
)

func materialVault(directory string) (*vault.Vault, error) {
	key, err := readMaterialFile(filepath.Join(filepath.Dir(directory), "configuration.key"), 32)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	return vault.FromKey(key)
}

// Material returns the node's current certificate and private key from the
// single on-disk identity format: identity/client.crt and identity/client.key.
func (s *State) Material() ([]byte, []byte, error) {
	certificate, err := os.ReadFile(filepath.Join(s.Directory, "client.crt"))
	if err != nil {
		return nil, nil, err
	}
	key, err := os.ReadFile(filepath.Join(s.Directory, "client.key"))
	return certificate, key, err
}

func LoadKeyPair(directory string) (tls.Certificate, error) {
	s, err := Load(directory)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificate, key, err := s.Material()
	defer clear(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	pair, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		return pair, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return tls.Certificate{}, err
	}
	nodeID, err := pki.NodeID(leaf)
	if err != nil || nodeID != s.NodeID {
		return tls.Certificate{}, errors.New("node certificate does not match retained node identity")
	}
	return pair, nil
}

// A long-lived control worker must not change its node identity when loading
// a renewed certificate. The fingerprint may change for the same node.
func LoadKeyPairForNode(directory, nodeID string) (tls.Certificate, error) {
	if nodeID == "" || len(nodeID) > 128 {
		return tls.Certificate{}, errors.New("retained node identity is required")
	}
	pair, err := LoadKeyPair(directory)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return tls.Certificate{}, err
	}
	actual, err := pki.NodeID(leaf)
	if err != nil || actual != nodeID {
		return tls.Certificate{}, errors.New("current certificate belongs to another retained node")
	}
	return pair, nil
}

func readMaterialFile(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, errors.New("invalid identity material file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || filelock.CheckSingleLink(f) != nil {
		return nil, errors.New("identity material file changed")
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		clear(data)
		return nil, errors.New("identity material exceeds limit")
	}
	return data, nil
}

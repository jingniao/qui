package identity

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/pki"
)

const rotationName = "pending-key-rotation.enc"

type keyRotation struct {
	Version     int    `json:"version"`
	NodeID      string `json:"node_id"`
	Previous    string `json:"previous_fingerprint"`
	PrivateKey  []byte `json:"private_key"`
	Certificate []byte `json:"certificate,omitempty"`
}

func (s *State) rotation() (keyRotation, error) {
	data, err := readMaterialFile(filepath.Join(s.Directory, rotationName), 1<<20)
	if err != nil {
		return keyRotation{}, err
	}
	if len(data) > 1<<20 {
		return keyRotation{}, errors.New("rotation exceeds limit")
	}
	v, err := materialVault(s.Directory)
	if err != nil {
		return keyRotation{}, err
	}
	plain, err := v.Open(data)
	if err != nil {
		return keyRotation{}, err
	}
	defer clear(plain)
	var stage keyRotation
	if err := json.Unmarshal(plain, &stage); err != nil {
		return stage, err
	}
	if stage.Version != 1 || stage.NodeID != s.NodeID || stage.Previous == "" {
		clear(stage.PrivateKey)
		return keyRotation{}, errors.New("rotation binding mismatch")
	}
	return stage, nil
}
func (s *State) saveRotation(stage keyRotation) error {
	v, err := materialVault(s.Directory)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(stage)
	if err != nil {
		return err
	}
	defer clear(plain)
	sealed, err := v.Seal(plain)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.Directory, rotationName), sealed)
}

// Persist the pending key before its first CSR request so retries retain it.
func (s *State) KeyRotationCSR() ([]byte, bool, error) {
	stage, err := s.rotation()
	if errors.Is(err, os.ErrNotExist) {
		old, err := s.currentCertificate()
		if err != nil {
			return nil, false, err
		}
		// A new pending key cannot make an expired or untrusted source identity
		// eligible for authenticated issuance. Check before creating any key.
		if err := s.validateRotationSource(old); err != nil {
			return nil, false, err
		}
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, false, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, false, err
		}
		defer clear(der)
		stage = keyRotation{Version: 1, NodeID: s.NodeID, Previous: pki.Fingerprint(old), PrivateKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})}
		current, err := s.currentCertificate()
		if err != nil || pki.Fingerprint(current) != stage.Previous {
			clear(stage.PrivateKey)
			return nil, false, errors.New("原身份在创建轮换期间变化，未保存候选")
		}
		if err := s.validateRotationSource(current); err != nil {
			clear(stage.PrivateKey)
			return nil, false, err
		}
		if err := s.saveRotation(stage); err != nil {
			clear(stage.PrivateKey)
			return nil, false, err
		}
	} else if err != nil {
		return nil, false, err
	}
	defer clear(stage.PrivateKey)
	if len(stage.Certificate) != 0 {
		return nil, true, nil
	}
	block, _ := pem.Decode(stage.PrivateKey)
	if block == nil {
		return nil, false, errors.New("invalid pending key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, false, err
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "proxy-agent"}}, key)
	if err != nil {
		return nil, false, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), false, nil
}

func (s *State) validateRotationSource(leaf *x509.Certificate) error {
	current, err := Load(s.Directory)
	if err != nil || current.NodeID != s.NodeID || current.LastAcked != s.LastAcked {
		return errors.New("原节点身份或ACK变化，未创建新轮换候选")
	}
	certificate, private, err := s.Material()
	defer clear(private)
	if err != nil {
		return err
	}
	if len(certificate) > 32768 || len(private) > 32768 {
		return errors.New("原轮换来源超过材料预算")
	}
	pair, err := tls.X509KeyPair(certificate, private)
	if err != nil || len(pair.Certificate) == 0 || !bytes.Equal(pair.Certificate[0], leaf.Raw) {
		return errors.New("原轮换来源证书与私钥不配对或已变化")
	}
	id, err := pki.NodeID(leaf)
	if err != nil || id != s.NodeID {
		return errors.New("原轮换来源不属于保留节点")
	}
	trust, err := s.TrustedNodeRoots()
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trust) {
		return errors.New("原轮换信任材料无效，未创建候选密钥")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return errors.New("原证书已过期、尚未生效或不属于当前批准信任，不能创建新轮换候选；保留原材料并核对Owner恢复路径")
	}
	return nil
}
func (s *State) validateRotation(stage keyRotation) (*x509.Certificate, error) {
	pair, err := tls.X509KeyPair(stage.Certificate, stage.PrivateKey)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	nodeID, err := pki.NodeID(leaf)
	if err != nil || nodeID != s.NodeID {
		return nil, errors.New("rotated identity mismatch")
	}
	trusted, err := s.TrustedNodeRoots()
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trusted) {
		return nil, errors.New("invalid trusted CA")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return nil, err
	}
	return leaf, nil
}
func (s *State) StageKeyRotation(certificate, ca []byte) error {
	stage, err := s.rotation()
	if err != nil {
		return err
	}
	defer clear(stage.PrivateKey)
	trusted, err := s.TrustedNodeCA(ca)
	if err != nil {
		return err
	}
	if !bytes.Equal(ca, trusted) || len(certificate) > 1<<20 {
		return errors.New("rotation cannot replace CA trust")
	}
	stage.Certificate = certificate
	leaf, err := s.validateRotation(stage)
	if err != nil {
		return err
	}
	old, err := s.currentCertificate()
	if err != nil {
		return err
	}
	if pki.Fingerprint(old) != stage.Previous || !leaf.NotAfter.After(old.NotAfter) {
		return errors.New("rotation source changed or validity was not extended")
	}
	oldKey, ok := old.PublicKey.(*ecdsa.PublicKey)
	newKey, newOK := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !newOK || oldKey.Equal(newKey) {
		return errors.New("rotation requires a different ECDSA key")
	}
	return s.saveRotation(stage)
}
func (s *State) CommitKeyRotation(version string) error {
	stage, err := s.rotation()
	if err != nil {
		return err
	}
	defer clear(stage.PrivateKey)
	leaf, err := s.validateRotation(stage)
	if err != nil {
		return err
	}
	old, err := s.currentCertificate()
	if err != nil {
		return err
	}
	if pki.Fingerprint(old) != stage.Previous && pki.Fingerprint(old) != pki.Fingerprint(leaf) {
		return errors.New("rotation conflicts with current identity")
	}
	// The pending-rotation journal retains the complete pair until the files
	// are replaced, so an interrupted commit is repeated from the journal.
	if err := atomicWrite(filepath.Join(s.Directory, "client.key"), stage.PrivateKey); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.Directory, "client.crt"), stage.Certificate)
}
func (s *State) FinishKeyRotation() error {
	stage, err := s.rotation()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer clear(stage.PrivateKey)
	current, err := s.currentCertificate()
	if err != nil {
		return err
	}
	leaf, err := s.validateRotation(stage)
	if err != nil {
		return err
	}
	if pki.Fingerprint(current) != pki.Fingerprint(leaf) {
		return errors.New("rotation is not the current identity")
	}
	return os.Remove(filepath.Join(s.Directory, rotationName))
}

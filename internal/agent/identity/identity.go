package identity

import (
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
	"sync"
	"time"
)

type State struct {
	ackObserved bool
	NodeID      string `json:"node_id"`
	LastAcked   uint64 `json:"last_acked"`
	Directory   string `json:"-"`
	mu          sync.Mutex
}

func CSR(directory string) ([]byte, error) {
	return csr(directory, true)
}

// RenewalCSR refuses a missing original key, including a deletion between a
// caller's metadata check and the read. Only initial enrollment creates keys.
func RenewalCSR(directory string) ([]byte, error) { return csr(directory, false) }

func csr(directory string, allowCreate bool) ([]byte, error) {
	if allowCreate {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
	}
	path := filepath.Join(directory, "client.key")
	var data []byte
	var err error
	if allowCreate {
		data, err = os.ReadFile(path)
	}
	if !allowCreate {
		state, loadErr := Load(directory)
		if loadErr != nil {
			return nil, loadErr
		}
		_, data, err = state.Material()
	}
	defer clear(data)
	var key *ecdsa.PrivateKey
	if errors.Is(err, os.ErrNotExist) && allowCreate {
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, errors.New("invalid local private key")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		var ok bool
		key, ok = parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("invalid node key type")
		}
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "proxy-agent"}}, key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

func SaveEnrollment(directory, nodeID string, certificate, ca []byte) error {
	initial := &State{Directory: directory, NodeID: nodeID}
	if err := initial.requireAckMaterial(0); err != nil {
		return err
	}
	key, err := os.ReadFile(filepath.Join(directory, "client.key"))
	if err != nil {
		return err
	}
	if _, err := tls.X509KeyPair(certificate, key); err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return errors.New("invalid node CA")
	}
	block, _ := pem.Decode(certificate)
	if block == nil {
		return errors.New("invalid node certificate")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	id, err := pki.NodeID(leaf)
	if err != nil || id != nodeID {
		return errors.New("certificate does not match assigned node identity")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(directory, "client.crt"), certificate); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(directory, "node-ca.crt"), ca); err != nil {
		return err
	}
	state := &State{NodeID: nodeID, Directory: directory}
	return state.Save(0)
}

func Load(directory string) (*State, error) {
	data, err := readState(filepath.Join(directory, "identity.json"))
	if err != nil {
		return nil, err
	}
	defer clear(data)
	original, err := parseAckIdentity(data)
	if err != nil {
		return nil, err
	}
	state := &State{Directory: directory, NodeID: original.NodeID, LastAcked: original.LastAcked, ackObserved: true}
	return state, nil
}

// ACK persistence atomically replaces this file while local status readers
// may be opening it. Only an OS sharing conflict warrants a short retry;
// missing files, permissions and malformed identities remain errors.
func readState(path string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		data, err := readMaterialFile(path, 4096)
		if !sharingConflict(err) || attempt == 5 {
			return data, err
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
}

func (s *State) Save(ack uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.NodeID == "" || len(s.NodeID) > 128 || ack < s.LastAcked || ack > uint64(1<<63-1) {
		return errors.New("invalid or regressing persisted acknowledgment")
	}
	if err := s.requireAckMaterial(ack); err != nil {
		return err
	}
	// Keep memory at the original confirmed position until file publication.
	// Use the existing two-field identity format, without copying the mutex.
	next := struct {
		NodeID    string `json:"node_id"`
		LastAcked uint64 `json:"last_acked"`
	}{s.NodeID, ack}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := publishAck(filepath.Join(s.Directory, "identity.json"), data); err != nil {
		return err
	}
	s.LastAcked = ack
	s.ackObserved = true
	return nil
}
func atomicWrite(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	var renameErr error
	for attempt := 0; attempt < 6; attempt++ {
		renameErr = os.Rename(temporary, path)
		if renameErr == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return renameErr
}

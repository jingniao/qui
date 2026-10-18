package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Authority struct {
	signingDisabled  bool
	historicalOnly   bool
	transitionSource *Authority
	Certificate      *x509.Certificate
	Key              *ecdsa.PrivateKey
	PEM              []byte
	Directory        string
}

func serial() (*big.Int, error) { return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128)) }
func privatePEM(key *ecdsa.PrivateKey) ([]byte, error) {
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}), nil
}

func LoadOrCreate(directory string) (*Authority, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	certPath, keyPath := filepath.Join(directory, "ca.crt"), filepath.Join(directory, "ca.key")
	certData, certErr := os.ReadFile(certPath)
	keyData, keyErr := os.ReadFile(keyPath)
	if errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		number, err := serial()
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		template := &x509.Certificate{SerialNumber: number, Subject: pkix.Name{CommonName: "Proxy Control node CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			return nil, err
		}
		keyData, err = privatePEM(key)
		if err != nil {
			return nil, err
		}
		certData = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		_, writeErr := file.Write(keyData)
		closeErr := file.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err := os.WriteFile(certPath, certData, 0600); err != nil {
			return nil, err
		}
	} else if certErr != nil || keyErr != nil {
		return nil, errors.New("node CA is incomplete or unreadable; preserve and repair its certificate/key pair")
	}
	return loadAuthority(directory, certData, keyData)
}

// LoadExisting never creates replacement CA material during restore checks.
func LoadExisting(directory string) (*Authority, error) {
	certData, err := os.ReadFile(filepath.Join(directory, "ca.crt"))
	if err != nil {
		return nil, err
	}
	keyData, err := os.ReadFile(filepath.Join(directory, "ca.key"))
	if err != nil {
		return nil, err
	}
	defer clear(keyData)
	return loadAuthorityMode(directory, certData, keyData)
}

func loadAuthority(directory string, certData, keyData []byte) (*Authority, error) {
	return loadAuthorityMode(directory, certData, keyData)
}

func loadAuthorityMode(directory string, certData, keyData []byte) (*Authority, error) {
	pair, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return nil, err
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	key, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok || !certificate.IsCA || time.Now().After(certificate.NotAfter) || time.Now().Before(certificate.NotBefore) {
		return nil, errors.New("invalid or expired node CA")
	}
	return loadActiveAuthorityMode(&Authority{Certificate: certificate, Key: key, PEM: certData, Directory: directory})
}

func (a *Authority) ExistingDevelopmentServer() (tls.Certificate, error) {
	if a == nil || a.historicalOnly || a.signingDisabled {
		return tls.Certificate{}, ErrCurrentSigningAuthority
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(a.Directory, "server.crt"), filepath.Join(a.Directory, "server.key"))
	if err != nil {
		return pair, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return pair, err
	}
	if !a.IssuedByTrustedCA(leaf) || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return pair, errors.New("invalid or expired development server identity")
	}
	return pair, nil
}

func ParseCSR(data []byte) (*x509.CertificateRequest, error) {
	if len(data) > 16384 {
		return nil, errors.New("CSR too large")
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE REQUEST" || len(rest) != 0 {
		return nil, errors.New("invalid CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, err
	}
	key, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("node keys must use ECDSA P-256")
	}
	return csr, nil
}

func (a *Authority) IssueNode(csr *x509.CertificateRequest, nodeID string) ([]byte, *x509.Certificate, error) {
	now := time.Now().UTC()
	if err := a.validateCurrentSigning(now); err != nil {
		return nil, nil, err
	}
	if csr == nil || csr.CheckSignature() != nil {
		return nil, nil, errors.New("invalid node CSR")
	}
	key, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() || nodeID == "" {
		return nil, nil, errors.New("node identity and ECDSA P-256 CSR required")
	}
	number, err := serial()
	if err != nil {
		return nil, nil, err
	}
	identity := &url.URL{Scheme: "spiffe", Host: "proxy-control", Path: "/nodes/" + nodeID}
	template := &x509.Certificate{SerialNumber: number, Subject: pkix.Name{CommonName: nodeID}, URIs: []*url.URL{identity}, NotBefore: now.Add(-time.Minute), NotAfter: minTime(now.Add(90*24*time.Hour), a.Certificate.NotAfter), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, a.Certificate, csr.PublicKey, a.Key)
	if err != nil {
		return nil, nil, err
	}
	certificate, err := x509.ParseCertificate(der)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), certificate, err
}

func (a *Authority) DevelopmentServer() (tls.Certificate, error) {
	if a == nil {
		return tls.Certificate{}, ErrCurrentSigningAuthority
	}
	path, keyPath := filepath.Join(a.Directory, "server.crt"), filepath.Join(a.Directory, "server.key")
	if _, err := os.Stat(path); err == nil {
		return a.ExistingDevelopmentServer()
	} else if !errors.Is(err, os.ErrNotExist) {
		return tls.Certificate{}, err
	}
	if a.transitionSource != nil {
		return tls.Certificate{}, errors.New("CA transition requires the retained development server identity")
	}
	now := time.Now().UTC()
	if err := a.validateCurrentSigning(now); err != nil {
		return tls.Certificate{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	number, err := serial()
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{SerialNumber: number, Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, NotBefore: now.Add(-time.Minute), NotAfter: minTime(now.AddDate(1, 0, 0), a.Certificate.NotAfter), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, a.Certificate, &key.PublicKey, a.Key)
	if err != nil {
		return tls.Certificate{}, err
	}
	private, err := privatePEM(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, private, 0600); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.LoadX509KeyPair(path, keyPath)
}

func Fingerprint(cert *x509.Certificate) string {
	hash := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(hash[:])
}
func NodeID(cert *x509.Certificate) (string, error) {
	if len(cert.URIs) != 1 {
		return "", errors.New("missing node certificate identity")
	}
	uri := cert.URIs[0]
	if uri.Scheme != "spiffe" || uri.Host != "proxy-control" || uri.Path != "/nodes/"+cert.Subject.CommonName || cert.Subject.CommonName == "" {
		return "", fmt.Errorf("invalid node certificate identity")
	}
	return cert.Subject.CommonName, nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

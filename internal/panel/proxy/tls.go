package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"proxycontrol/pkg/contracts/desiredstate"
	"time"
)

func managedTLS(serverName string) (desiredstate.ProtocolSettings, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return desiredstate.ProtocolSettings{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return desiredstate.ProtocolSettings{}, err
	}
	now := time.Now().UTC()
	certificate := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: serverName}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if ip := net.ParseIP(serverName); ip != nil {
		certificate.IPAddresses = []net.IP{ip}
	} else {
		certificate.DNSNames = []string{serverName}
	}
	encoded, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		return desiredstate.ProtocolSettings{}, err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return desiredstate.ProtocolSettings{}, err
	}
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: encoded}))
	return desiredstate.ProtocolSettings{Security: "tls", ServerName: serverName, CertificatePEM: cert, PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), TrustedCAPEM: cert}, nil
}

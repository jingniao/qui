package desiredstate

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func relayTestMaterial(t *testing.T) (GostRelayTLS, GostHop) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}))
	return GostRelayTLS{CertificatePEM: certPEM, PrivateKeyPEM: keyPEM, ClientCAPEM: certPEM}, GostHop{Host: "127.0.0.1", Port: 22000, ServerName: "exit.test", CACertPEM: certPEM, ClientCertPEM: certPEM, ClientKeyPEM: keyPEM}
}

func TestMiddleRelayContract(t *testing.T) {
	material, hop := relayTestMaterial(t)
	for _, test := range []struct {
		name  string
		edit  func(*ForwardSettings)
		valid bool
	}{
		{"middle", func(*ForwardSettings) {}, true},
		{"exit", func(f *ForwardSettings) { f.Hops = nil }, true},
		{"too many next hops", func(f *ForwardSettings) { f.Hops = append(f.Hops, hop) }, false},
		{"missing listener material", func(f *ForwardSettings) { f.Relay = nil }, false},
		{"missing client key", func(f *ForwardSettings) { f.Hops[0].ClientKeyPEM = "" }, false},
		{"missing server name", func(f *ForwardSettings) { f.Hops[0].ServerName = "" }, false},
		{"invalid address", func(f *ForwardSettings) { f.Hops[0].Port = 0 }, false},
		{"target pool", func(f *ForwardSettings) { f.Targets = []ForwardTarget{{Host: "localhost", Port: 80}} }, false},
		{"network", func(f *ForwardSettings) { f.Network = "udp" }, false},
		{"pool option", func(f *ForwardSettings) { f.MaxFails = gostInt(1) }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &ForwardSettings{Mode: GostForwardModeRelay, Relay: &material, Hops: []GostHop{hop}}
			test.edit(f)
			if err := f.Validate(); (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
			if err := ValidateForwardCore(CoreXray, ProtocolSettings{Forward: f}); err == nil {
				t.Fatal("non-GOST core accepted relay settings")
			}
		})
	}
}

func TestGostContractCapabilityGate(t *testing.T) {
	material, hop := relayTestMaterial(t)
	middle := &ForwardSettings{Mode: GostForwardModeRelay, Relay: &material, Hops: []GostHop{hop}}
	for version := uint32(0); version <= GostContractVersion; version++ {
		err := middle.ValidateGostContract(version)
		if version < 4 && (err == nil || !strings.Contains(err.Error(), "gost_contract_version >= 4")) {
			t.Fatalf("version %d: %v", version, err)
		}
		if version == 4 && err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []*ForwardSettings{
		{Mode: GostForwardModeRelay, Relay: &material},
		{Host: "localhost", Port: 80, Network: "tcp", Hops: []GostHop{hop, hop}},
	} {
		if err := f.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := f.ValidateGostContract(3); err != nil {
			t.Fatalf("contract-3 regression: %v", err)
		}
		if err := f.ValidateGostContract(2); err == nil {
			t.Fatal("contract 2 accepted relay")
		}
	}
}

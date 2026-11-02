package gost

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

type testLeaf struct{ cert, key string }

// testCA issues one CA and the two leaves a tunnel uses, so a renderer test never
// needs the panel.
func testCA(t *testing.T, serverName string) (testLeaf, testLeaf, testLeaf) {
	t.Helper()
	now := time.Now().UTC()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test ca"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leaf := func(serial int64, dns []string, usage x509.ExtKeyUsage) testLeaf {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "leaf"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), DNSNames: dns, ExtKeyUsage: []x509.ExtKeyUsage{usage}, KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true}
		der, err := x509.CreateCertificate(rand.Reader, template, caCertificate, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		private, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return testLeaf{cert: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), key: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))}
	}
	return testLeaf{cert: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))}, leaf(2, []string{serverName}, x509.ExtKeyUsageServerAuth), leaf(3, nil, x509.ExtKeyUsageClientAuth)
}

func entryState(t *testing.T, hops int) desiredstate.State {
	t.Helper()
	ca, _, client := testCA(t, "t-test.gost.qui.internal")
	settings := &desiredstate.ForwardSettings{Host: "192.0.2.10", Port: 443, Network: "tcp"}
	for index := 0; index < hops; index++ {
		settings.Hops = append(settings.Hops, desiredstate.GostHop{Host: "198.51.100." + string(rune('1'+index)), Port: 20000 + index, ServerName: "t-test.gost.qui.internal", CACertPEM: ca.cert, ClientCertPEM: client.cert, ClientKeyPEM: client.key})
	}
	return desiredstate.State{
		SchemaVersion: 1,
		Revision:      7,
		NodeID:        "entry",
		Users:         []desiredstate.User{},
		Credentials:   []desiredstate.Credential{},
		Inbounds: []desiredstate.Inbound{{
			ID: "fwd-entry", Core: "gost", Protocol: "forward", Listen: "0.0.0.0", Port: 20060, Enabled: true, DefaultOutbound: "direct",
			Settings: desiredstate.ProtocolSettings{Forward: settings},
		}},
		Outbounds:      []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
		OutboundGroups: []desiredstate.OutboundGroup{},
		ProxyChains:    []desiredstate.ProxyChain{},
		Policies:       []desiredstate.Policy{},
		Logging:        desiredstate.Logging{DomainOnly: true},
	}
}

func relayState(t *testing.T) desiredstate.State {
	t.Helper()
	ca, server, _ := testCA(t, "t-test.gost.qui.internal")
	state := entryState(t, 0)
	state.NodeID = "exit"
	state.Inbounds = []desiredstate.Inbound{{
		ID: "relay-exit", Core: "gost", Protocol: "forward", Listen: "0.0.0.0", Port: 22000, Enabled: true, DefaultOutbound: "direct",
		Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Mode: desiredstate.GostForwardModeRelay, Relay: &desiredstate.GostRelayTLS{CertificatePEM: server.cert, PrivateKeyPEM: server.key, ClientCAPEM: ca.cert}}},
	}}
	return state
}

func renderState(t *testing.T, state desiredstate.State) (Config, core.RenderedConfig) {
	t.Helper()
	adapter := &Adapter{Directory: t.TempDir(), NodeID: state.NodeID}
	rendered, err := adapter.Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{}
	if err := json.Unmarshal(rendered.Data, &config); err != nil {
		t.Fatal(err)
	}
	return config, rendered
}

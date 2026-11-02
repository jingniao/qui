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
	"os"
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

// TestRenderKeepsOneChainForAPortRange covers the entry half of a tunnel that
// listens on a port range: every expanded service dials through one chain. A chain
// built inside the port loop would repeat its name once per port, which GOST
// refuses.
func TestRenderKeepsOneChainForAPortRange(t *testing.T) {
	state := entryState(t, 1)
	end := 20061
	state.Inbounds[0].Settings.Forward.PortEnd = &end
	config, _ := renderState(t, state)
	if len(config.Chains) != 1 || config.Chains[0].Name != chainName("fwd-entry") {
		t.Fatalf("chains: %+v", config.Chains)
	}
	if len(config.Services) != 2 {
		t.Fatalf("services: %+v", config.Services)
	}
	for _, service := range config.Services {
		if service.Handler.Chain != config.Chains[0].Name {
			t.Fatalf("service %s does not dial through the chain: %+v", service.Name, service.Handler)
		}
	}
}

// TestRotatedMaterialKeepsTheFilesOfTheOldRevision covers a rollback after a
// credential rotation: the files one revision was rendered with are keyed by the
// material itself, so rendering the next generation leaves the previous one intact
// for the configuration that still points at it.
func TestRotatedMaterialKeepsTheFilesOfTheOldRevision(t *testing.T) {
	adapter := &Adapter{Directory: t.TempDir(), NodeID: "exit"}
	first, second := relayState(t), relayState(t)
	renderedFirst, err := adapter.Render(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Render(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	material := func(rendered core.RenderedConfig) (string, string) {
		t.Helper()
		config := Config{}
		if err := json.Unmarshal(rendered.Data, &config); err != nil {
			t.Fatal(err)
		}
		paths := config.Services[0].Listener.TLS
		if paths == nil {
			t.Fatal("relay listener has no material")
		}
		certificate, err := os.ReadFile(paths.CertFile)
		if err != nil {
			t.Fatal(err)
		}
		key, err := os.ReadFile(paths.KeyFile)
		if err != nil {
			t.Fatal(err)
		}
		return string(certificate), string(key)
	}
	certificate, key := material(renderedFirst)
	relay := first.Inbounds[0].Settings.Forward.Relay
	if certificate != relay.CertificatePEM || key != relay.PrivateKeyPEM {
		t.Fatal("a rendered credential generation overwrote the files of the earlier revision")
	}
}

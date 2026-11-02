package proxy

import (
	"crypto/x509"
	"encoding/json"
	"strings"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

func serverCertificate(t *testing.T, material gostMaterial) *x509.Certificate {
	t.Helper()
	certificate, err := x509.ParseCertificate(pemBlock(material.ServerCertPEM))
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

// The exit relay presents the server leaf and requires the client leaf; both are
// signed by the tunnel CA, and the server carries the synthetic name the entry
// dials.
func TestGostMaterialIsUsableMutualTLS(t *testing.T) {
	material, err := newGostMaterial("tunnel-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := material.valid("tunnel-a"); err != nil {
		t.Fatal(err)
	}
	if material.Generation != 1 || material.ServerName != gostServerName("tunnel-a") {
		t.Fatalf("unexpected material identity %+v", material)
	}
	if !strings.HasSuffix(material.ServerName, ".gost.qui.internal") || len(material.ServerName) > 253 {
		t.Fatalf("server name %q is not a stable synthetic name", material.ServerName)
	}
	if material.Fingerprint == "" || material.NotAfter == "" {
		t.Fatal("material carries no expiry or fingerprint")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(material.CACertPEM)) {
		t.Fatal("ca is not usable")
	}
	server := serverCertificate(t, material)
	if server.DNSNames[0] != material.ServerName {
		t.Fatalf("server SAN %v does not match the dialed name", server.DNSNames)
	}
	if _, err := server.Verify(x509.VerifyOptions{Roots: pool, DNSName: material.ServerName, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("server leaf does not verify: %v", err)
	}
	client, err := x509.ParseCertificate(pemBlock(material.ClientCertPEM))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("client leaf does not verify: %v", err)
	}
	// The two EKUs stay apart: a client certificate must not pass as the server.
	if _, err := client.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err == nil {
		t.Fatal("the client leaf is also accepted as a server certificate")
	}
}

// A certificate of another tunnel is not trusted: the relay of tunnel A must
// reject a client of tunnel B, which is the point of the client CA.
func TestGostMaterialRejectsAnotherTunnelsCertificate(t *testing.T) {
	first, err := newGostMaterial("tunnel-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newGostMaterial("tunnel-b", 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.CACertPEM == second.CACertPEM || first.Fingerprint == second.Fingerprint {
		t.Fatal("two tunnels share material")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(first.CACertPEM)) {
		t.Fatal("ca is not usable")
	}
	foreign := serverCertificate(t, second)
	if _, err := foreign.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err == nil {
		t.Fatal("a certificate of another tunnel verified against this CA")
	}
	// The material is bound to its tunnel: the name is derived from the id, so a
	// set cannot be replayed onto another tunnel.
	if err := first.valid("tunnel-b"); err == nil {
		t.Fatal("material of one tunnel was accepted for another")
	}
}

// Everything the API can show is redacted: the relay key and the client key stay
// out of the inbound views, and the tunnel only reports metadata.
func TestGostRelayMaterialIsRedacted(t *testing.T) {
	material, err := newGostMaterial("tunnel-a", 3)
	if err != nil {
		t.Fatal(err)
	}
	settings := desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{
		Mode:  desiredstate.GostForwardModeRelay,
		Relay: &desiredstate.GostRelayTLS{CertificatePEM: material.ServerCertPEM, PrivateKeyPEM: material.ServerKeyPEM, ClientCAPEM: material.CACertPEM},
	}}
	if mask(settings).Forward.Relay.PrivateKeyPEM != "" {
		t.Fatal("the relay key survived redaction")
	}
	entry := desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "192.0.2.10", Port: 443, Network: "tcp", Hops: []desiredstate.GostHop{{Host: "198.51.100.1", Port: 20000, ServerName: material.ServerName, CACertPEM: material.CACertPEM, ClientCertPEM: material.ClientCertPEM, ClientKeyPEM: material.ClientKeyPEM}}}}
	if mask(entry).Forward.Hops[0].ClientKeyPEM != "" {
		t.Fatal("the hop client key survived redaction")
	}
	view, err := json.Marshal(&CanvasTunnelCredentials{Generation: material.Generation, ServerName: material.ServerName, NotAfter: material.NotAfter, Fingerprint: material.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(view), "BEGIN") || strings.Contains(string(view), "PRIVATE") {
		t.Fatalf("the credential view carries material: %s", view)
	}
}

// The public inbound API refuses the relay halves and the canvas path accepts
// them, so an Owner cannot edit a tunnel's chain by hand.
func TestGostRelaySettingsAreCanvasOnly(t *testing.T) {
	material, err := newGostMaterial("tunnel-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	relay := InboundInput{Name: "relay", NodeID: "exit", Protocol: "forward", Listen: "0.0.0.0", Port: 22000, DefaultOutbound: "direct",
		Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Mode: desiredstate.GostForwardModeRelay, Relay: &desiredstate.GostRelayTLS{CertificatePEM: material.ServerCertPEM, PrivateKeyPEM: material.ServerKeyPEM, ClientCAPEM: material.CACertPEM}}}}
	if err := ValidateInbound(relay); err == nil {
		t.Fatal("the public API accepted relay material")
	}
	if err := validateGeneratedInbound(relay); err != nil {
		t.Fatalf("the canvas path refused valid relay material: %v", err)
	}
	broken := relay
	broken.Settings = desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Mode: desiredstate.GostForwardModeRelay}}
	if err := validateGeneratedInbound(broken); err == nil {
		t.Fatal("a relay without material was accepted")
	}
}

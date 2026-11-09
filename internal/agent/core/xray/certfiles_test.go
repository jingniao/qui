package xray

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/desiredstate"
)

// A node-side file pair reaches Xray as certificateFile/keyFile: the Agent proves
// the files and the private key never becomes part of the document.
func TestRenderUsesNodeCertificateFiles(t *testing.T) {
	pki := testutil.Certificates(t)
	directory := filepath.Dir(pki.Server.Certificate)
	previous := core.CertificateDirectory
	core.CertificateDirectory = directory
	t.Cleanup(func() { core.CertificateDirectory = previous })
	// The settings always carry the canonical node paths; the fixture directory
	// holds files with the same names.
	certificatePath, keyPath := desiredstate.CertificateDirectory+"/server.crt", desiredstate.CertificateDirectory+"/server.key"
	state := desiredstate.State{
		SchemaVersion: 1, Revision: 1, NodeID: "cert-node", Logging: desiredstate.Logging{DomainOnly: true},
		Users:       []desiredstate.User{{ID: "user", Enabled: true}},
		Credentials: []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "vless", Username: "cert-user", Secret: "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"}},
		Inbounds: []desiredstate.Inbound{{ID: "entry", Protocol: "vless", Listen: "127.0.0.1", Port: 24443, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"credential"},
			Settings: desiredstate.ProtocolSettings{Security: "tls", ServerName: "localhost", CertificatePath: certificatePath, PrivateKeyPath: keyPath}}},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}},
	}
	rendered, err := Render(context.Background(), state)
	if err != nil {
		t.Fatalf("a valid file pair was refused at render: %v", err)
	}
	var doc configDocument
	if err := json.Unmarshal(rendered.Data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Inbounds[0].Stream == nil || doc.Inbounds[0].Stream.TLS == nil || len(doc.Inbounds[0].Stream.TLS.Certificates) != 1 {
		t.Fatalf("the TLS material was not rendered: %+v", doc.Inbounds[0].Stream)
	}
	certificate := doc.Inbounds[0].Stream.TLS.Certificates[0]
	if certificate.CertificateFile != certificatePath || certificate.KeyFile != keyPath {
		t.Fatalf("the file pair was not rendered: %+v", certificate)
	}
	if len(certificate.Certificate) != 0 || len(certificate.Key) != 0 {
		t.Fatal("the inline certificate branch also ran")
	}
	broken := state
	broken.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
	broken.Inbounds[0].Settings.CertificatePath = desiredstate.CertificateDirectory + "/absent.crt"
	if _, err := Render(context.Background(), broken); err == nil {
		t.Fatal("a missing certificate file was rendered")
	}
}

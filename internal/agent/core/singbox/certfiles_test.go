package singbox

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/testutil"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// A node-side file pair reaches the core as its own certificate_path/key_path
// fields: the Agent proves the files and the private key never becomes part of
// the document.
func TestRenderUsesNodeCertificateFiles(t *testing.T) {
	pki := testutil.Certificates(t)
	directory := filepath.Dir(pki.Server.Certificate)
	previous := core.CertificateDirectory
	core.CertificateDirectory = directory
	t.Cleanup(func() { core.CertificateDirectory = previous })
	// The settings always carry the canonical node paths; the fixture directory
	// holds files with the same names.
	certificatePath, keyPath := desiredstate.CertificateDirectory+"/server.crt", desiredstate.CertificateDirectory+"/server.key"
	state := stateFixture(t, "trojan", 24443)
	state.Inbounds[0].Settings = desiredstate.ProtocolSettings{Security: "tls", ServerName: "localhost", CertificatePath: certificatePath, PrivateKeyPath: keyPath}
	rendered, err := Render(context.Background(), state, time.Now())
	if err != nil {
		t.Fatalf("a valid file pair was refused at render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(rendered.Data, &doc); err != nil {
		t.Fatal(err)
	}
	options := doc.Inbounds[0].TLS
	if options == nil || options.CertificatePath != certificatePath || options.KeyPath != keyPath {
		t.Fatalf("the file pair was not rendered: %+v", options)
	}
	if len(options.Certificate) != 0 || len(options.Key) != 0 {
		t.Fatal("the inline certificate branch also ran")
	}
	adapter := &Adapter{}
	adapter.applyUpstreamCapabilities(&pb.Capabilities{CoreName: "sing-box", TlsCertificateFilesSupported: true})
	if _, err := adapter.Render(context.Background(), state); err != nil {
		t.Fatalf("adapter.Render refused a valid node certificate file pair after capability detection: %v", err)
	}
	broken := stateFixture(t, "trojan", 24444)
	broken.Inbounds[0].Settings = desiredstate.ProtocolSettings{Security: "tls", ServerName: "localhost", CertificatePath: desiredstate.CertificateDirectory + "/absent.crt", PrivateKeyPath: keyPath}
	if _, err := Render(context.Background(), broken, time.Now()); err == nil {
		t.Fatal("a missing certificate file was rendered")
	}
}

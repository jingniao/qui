package deployment

import (
	"context"
	"strings"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

// The publish path repeats the node-certificate-file gate: a revision that names
// files for a core which never reported the renderer is refused, exactly like the
// xhttp/Reality contract, so an older Agent cannot receive the two paths.
func TestDeployRefusesCertificateFilesTheCoreCannotRender(t *testing.T) {
	r, _, _, rev := coreNodeFixture(t, "linux/amd64", "xray")
	ctx := context.Background()
	state, err := r.State(ctx, rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	state.Credentials = []desiredstate.Credential{{ID: "vless-credential", Protocol: "vless", Secret: "3f8b1a8a-1b2c-4d5e-8f90-1234567890ab"}}
	state.Inbounds = []desiredstate.Inbound{{
		ID: "file-in", Protocol: "vless", Listen: "0.0.0.0", Port: 24443, Enabled: true, DefaultOutbound: "direct",
		CredentialIDs: []string{"vless-credential"},
		Settings:      desiredstate.ProtocolSettings{Security: "tls", CertificatePath: desiredstate.CertificateDirectory + "/node.crt", PrivateKeyPath: desiredstate.CertificateDirectory + "/node.key"},
	}}
	draft, err := r.Create(ctx, state, "owner", "certificate file inbound")
	if err != nil {
		t.Fatalf("the typed settings were refused before the capability gate: %v", err)
	}
	if err := r.Deploy(ctx, draft.ID); err == nil || !strings.Contains(err.Error(), "certificate file") {
		t.Fatalf("a revision the Agent cannot render was published: %v", err)
	}
}

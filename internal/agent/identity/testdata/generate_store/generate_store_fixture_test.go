package identity

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/agentstorage"
)

// TestGenerateWrite2StoreFixture is a generator, not a test. Run it once with
// STORE_FIXTURE_OUT set to write a Write=2 agent store produced by the current
// code into the fixture directory. The store contains only test-generated keys.
func TestGenerateWrite2StoreFixture(t *testing.T) {
	out := os.Getenv("STORE_FIXTURE_OUT")
	if out == "" {
		t.Skip("STORE_FIXTURE_OUT not set; generator skipped")
	}
	const nodeID = "fixture-node-0001"
	root := t.TempDir()
	identityDir := filepath.Join(root, "identity")
	if err := os.MkdirAll(identityDir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Open(filepath.Join(root, "configuration.key")); err != nil {
		t.Fatal(err)
	}
	caDir := filepath.Join(root, "ca")
	authority, err := pki.LoadOrCreate(caDir)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM, err := CSR(identityDir)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(csrPEM)
	if err != nil {
		t.Fatal(err)
	}
	certificate, _, err := authority.IssueNode(csr, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile(filepath.Join(caDir, "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveEnrollment(identityDir, nodeID, certificate, ca); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Ensure(root, nodeID, agentstorage.Current("1.0.0")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	copyFixtureFile(t, filepath.Join(root, "configuration.key"), filepath.Join(out, "configuration.key"))
	if err := os.MkdirAll(filepath.Join(out, "identity"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"client.key", "client.crt", "node-ca.crt", "identity.json", "agent-storage-contract.enc"} {
		copyFixtureFile(t, filepath.Join(identityDir, name), filepath.Join(out, "identity", name))
	}
	t.Logf("wrote Write=2 store fixture to %s", out)
}

func copyFixtureFile(t *testing.T, source, target string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"proxycontrol/pkg/contracts/agentstorage"
	"proxycontrol/pkg/contracts/release"
	"runtime"
	"testing"
	"time"
)

func signedAgentFixture(t *testing.T, version string) (Candidate, release.Policy) {
	t.Helper()
	path := os.Getenv("AGENT_BINARY")
	if path == "" {
		t.Skip("set actual Agent for fixed compatibility probe")
	}
	artifact, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifact)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	signed, err := release.Sign(release.Manifest{SchemaVersion: 1, Component: "proxy-agent", Version: version, OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: 1, Bytes: int64(len(artifact)), SHA256: hex.EncodeToString(sum[:]), IssuedAt: at, ExpiresAt: at.Add(time.Hour)}, private)
	if err != nil {
		t.Fatal(err)
	}
	policy := release.Policy{Component: "proxy-agent", OS: runtime.GOOS, Architecture: runtime.GOARCH, Now: at, Keys: map[string]release.TrustedKey{signed.KeyID: {PublicKey: public, Components: []string{"proxy-agent"}}}}
	candidate, err := Stage(context.Background(), t.TempDir(), signed, policy, bytes.NewReader(artifact))
	if err != nil {
		t.Fatal(err)
	}
	return candidate, policy
}
func TestRealSignedAgentFixedStorageProbeVersionTrustAndIdentity(t *testing.T) {
	candidate, policy := signedAgentFixture(t, "1.0.0-storage-test")
	current := agentstorage.Current("current-agent")
	probe, err := PreflightAgent(context.Background(), t.TempDir(), candidate, policy, current)
	if err != nil || probe.Contract.AgentVersion != candidate.Manifest.Version || probe.Image.SHA256 != candidate.Manifest.SHA256 || probe.Image.Sequence != 1 {
		t.Fatal(probe, err)
	}
	// Materializing/probing is not accepting or installing a release.
	options, _, _, _ := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if h.head.Number != 0 {
		t.Fatal("fixture already has accepted history")
	}
	if _, err := PreflightAgent(context.Background(), t.TempDir(), candidate, policy, current); err != nil {
		t.Fatal(err)
	}
	if h.head.Number != 0 {
		t.Fatal("probe wrote release history")
	}
	candidate2, policy2 := signedAgentFixture(t, "different-version")
	if _, err := PreflightAgent(context.Background(), t.TempDir(), candidate2, policy2, current); err == nil {
		t.Fatal("native version differs from signed Agent version")
	}
	key := policy.Keys[candidate.Signed.KeyID]
	key.Revoked = true
	policy.Keys[candidate.Signed.KeyID] = key
	folder := filepath.Join(t.TempDir(), "untrusted")
	if _, err := PreflightAgent(context.Background(), folder, candidate, policy, current); err == nil {
		t.Fatal("revoked candidate executed")
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("untrusted probe materialized executable")
	}
}
func TestAgentProbeRejectsUnsafeContractsAndMalformedImmutableCandidate(t *testing.T) {
	candidate, policy := signedAgentFixture(t, "1.0.0-storage-test")
	current := agentstorage.Current("current")
	bad := candidate
	bad.Manifest.Component = "xray"
	if _, err := PreflightAgent(context.Background(), t.TempDir(), bad, policy, current); err == nil {
		t.Fatal("non-Agent candidate probed")
	}
	bad = candidate
	bad.SHA256 = "changed"
	if _, err := PreflightAgent(context.Background(), t.TempDir(), bad, policy, current); err == nil {
		t.Fatal("changed immutable candidate probed")
	}
	future := current
	future.Formats.ReleaseHistory = agentstorage.Format{ReadMax: 4, Write: 4}
	if _, err := PreflightAgent(context.Background(), t.TempDir(), candidate, policy, future); err == nil {
		t.Fatal("candidate accepted data it cannot read")
	}
}

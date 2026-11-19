package agenthost

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/release"
)

func TestLocalInstallAcceptsVerifiedCandidateWithoutAcceptingPreview(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	history, err := updater.CreateHistory(updater.HistoryOptions{Directory: filepath.Join(root, "history"), Checkpoint: filepath.Join(root, "identity", "checkpoint"), NodeID: "node", Key: bytes.Repeat([]byte{4}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.BootstrapImage(ctx, "proxy-agent", updater.Image{SHA256: strings.Repeat("b", 64), Bytes: 100, Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifact)
	now := time.Now().UTC()
	signed, err := release.Sign(release.Manifest{SchemaVersion: 1, Component: "proxy-agent", Version: "2.0.0", OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: 1, Bytes: int64(len(artifact)), SHA256: hex.EncodeToString(sum[:]), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}, private)
	if err != nil {
		t.Fatal(err)
	}
	policy := release.Policy{Component: "proxy-agent", OS: runtime.GOOS, Architecture: runtime.GOARCH, Now: now, Keys: map[string]release.TrustedKey{signed.KeyID: {PublicKey: public, Components: []string{"proxy-agent"}}}}
	if _, err := updater.Stage(ctx, localCandidates(root), signed, policy, bytes.NewReader(artifact)); err != nil {
		t.Fatal(err)
	}
	if _, err := stageLocalCandidate(ctx, root, signed, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := history.BeginInstallation(ctx, "preview", 1, signed, policy); !errors.Is(err, updater.ErrInstallation) {
		t.Fatalf("preview accepted release: %v", err)
	}
	runner := &jobRunner{ctx: ctx, directory: root, nodeID: "node", history: history}
	untrusted := signed
	untrusted.Signature = "invalid"
	if _, err := runner.acceptLocalCandidate(ctx, untrusted, policy); err == nil {
		t.Fatal("untrusted local candidate accepted")
	}
	if _, err := history.BeginInstallation(ctx, "untrusted", 1, signed, policy); !errors.Is(err, updater.ErrInstallation) {
		t.Fatalf("failed verification recorded acceptance: %v", err)
	}
	candidate, err := runner.acceptLocalCandidate(ctx, signed, policy)
	if err != nil {
		t.Fatal(err)
	}
	state, err := history.BeginInstallation(ctx, "local-install", 1, candidate.Signed, policy)
	if err != nil || state.Phase != "switching" {
		t.Fatalf("verified local install rejected: phase=%s err=%v", state.Phase, err)
	}
}

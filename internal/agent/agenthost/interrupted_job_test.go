package agenthost

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/release"
)

func TestStartedWithoutInstallationClosesAttemptWithoutExecution(t *testing.T) {
	root := t.TempDir()
	history, err := updater.CreateHistory(updater.HistoryOptions{Directory: filepath.Join(root, "history"), Checkpoint: filepath.Join(root, "identity", "checkpoint"), NodeID: "node", Key: bytes.Repeat([]byte{4}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	image := updater.Image{SHA256: strings.Repeat("b", 64), Bytes: 100, Version: "1.0"}
	if err := history.BootstrapImage(context.Background(), "proxy-agent", image); err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	signed, err := release.Sign(release.Manifest{SchemaVersion: 1, Component: "proxy-agent", OS: "windows", Architecture: "amd64", Version: "2.0", Sequence: 1, Bytes: 100, SHA256: strings.Repeat("a", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}, private)
	if err != nil {
		t.Fatal(err)
	}
	request := release.AgentUpdateRequest{SchemaVersion: 1, ID: "interrupted", NodeID: "node", Revision: 1, Operation: "install", Release: signed}
	// No process, configuration, downloader or installed candidate is provided:
	// an authenticated Started request must be closed before using any of them.
	runner := &jobRunner{ctx: context.Background(), nodeID: "node", history: history}
	result, err := runner.execute(agentjobs.Job{Request: request, Started: true})
	if err != nil || result.Status != "failed_closed" || result.Code != "installation_failed" || result.InstalledDigest != image.SHA256 || result.InstalledVersion != image.Version {
		t.Fatal("interrupted request was not bound to confirmed baseline", result, err)
	}
	if _, found, err := history.InstallationRequest(request.ID); err != nil || found {
		t.Fatal("closing interrupted request created installation", err)
	}
	current, _, err := history.Installation("proxy-agent")
	if err != nil || current.Phase != "ready" || current.Current != image {
		t.Fatal("closing interrupted request changed confirmed image", current, err)
	}
}

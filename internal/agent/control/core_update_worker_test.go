package control

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/config"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/release"
	"proxycontrol/pkg/contracts/scheduler"
)

// fakeCoreRuntime is a core.BinaryRuntime that never runs a real core. It
// reports a configurable version and health, and records which executable was
// selected.
type fakeCoreRuntime struct {
	name      string
	version   string
	badPath   string
	selected  string
	activate  int
	applied   []core.RenderedConfig
	lastError error
}

func (a *fakeCoreRuntime) Name() string { return a.name }
func (a *fakeCoreRuntime) DetectCapabilities(context.Context) (*pb.Capabilities, error) {
	return &pb.Capabilities{CoreName: a.name, CoreVersion: a.version}, nil
}
func (a *fakeCoreRuntime) Render(_ context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	data, err := json.Marshal(state)
	return core.RenderedConfig{Revision: state.Revision, Data: data}, err
}
func (a *fakeCoreRuntime) Validate(context.Context, core.RenderedConfig) error { return nil }
func (a *fakeCoreRuntime) Apply(_ context.Context, configuration core.RenderedConfig) (core.ApplyResult, error) {
	a.applied = append(a.applied, configuration)
	return core.ApplyResult{Revision: configuration.Revision, Success: true, Code: "success"}, nil
}
func (a *fakeCoreRuntime) Rollback(context.Context, int64) error { return nil }
func (a *fakeCoreRuntime) Health(context.Context) (core.HealthStatus, error) {
	if a.badPath != "" && a.selected == a.badPath {
		return core.HealthStatus{}, errors.New("updated core health check failed")
	}
	return core.HealthStatus{Healthy: true, CheckedAt: time.Now().UTC()}, nil
}
func (a *fakeCoreRuntime) CollectStats(context.Context) (core.StatsBatch, error) {
	return core.StatsBatch{}, nil
}
func (a *fakeCoreRuntime) ConfigureRuntime(core.RuntimeHooks)          {}
func (a *fakeCoreRuntime) Close() error                                { return nil }
func (a *fakeCoreRuntime) SampleTraffic(context.Context) error         { return nil }
func (a *fakeCoreRuntime) SupportsAccountTelemetry() bool              { return false }
func (a *fakeCoreRuntime) OutboundHealth() map[string]scheduler.Health { return nil }
func (a *fakeCoreRuntime) ExecutablePath() string                      { return a.selected }
func (a *fakeCoreRuntime) DetectVersion(context.Context, string) (string, error) {
	return a.version, a.lastError
}
func (a *fakeCoreRuntime) ValidateExecutable(context.Context, string, core.RenderedConfig) (string, error) {
	return a.version, a.lastError
}
func (a *fakeCoreRuntime) SelectExecutable(_ context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	a.selected, a.activate = path, a.activate+1
	return nil
}

// updateHarness builds a node whose core installation history, current
// revision and configuration key are real, while the core itself is faked.
func updateHarness(t *testing.T, adapter *fakeCoreRuntime, options ...func(string)) (*coreUpdates, config.Agent, *reconcile.Reconciler, string) {
	t.Helper()
	root := t.TempDir()
	dataDirectory := filepath.Join(root, "agent")
	if err := os.MkdirAll(filepath.Join(dataDirectory, "identity"), 0700); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x5a}, 32)
	if err := os.WriteFile(filepath.Join(dataDirectory, "configuration.key"), key, 0600); err != nil {
		t.Fatal(err)
	}
	history, err := updater.CreateHistory(updater.HistoryOptions{Directory: filepath.Join(dataDirectory, "release-history"), Checkpoint: filepath.Join(dataDirectory, "identity", "release-checkpoint.enc"), NodeID: "node-update", Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if err := history.Close(); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, "original-core"+coreExecutableSuffix())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, payload, 0500); err != nil {
		t.Fatal(err)
	}
	adapter.name, adapter.selected = "xray", original
	manager, err := reconcile.New(dataDirectory, "node-update", adapter)
	if err != nil {
		t.Fatal(err)
	}
	manager.RequireSuccessReceipts = false
	state := desiredstate.State{SchemaVersion: 1, NodeID: "node-update", Revision: 1, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: "user", Enabled: true}}, Credentials: []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "http", Username: "update-user", Secret: "update-device-password"}}, Inbounds: []desiredstate.Inbound{{ID: "entry", Protocol: "http", Listen: "127.0.0.1", Port: freeUpdatePort(t), Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"credential"}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	if _, err := manager.Apply(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	cfg := config.Agent{DataDirectory: dataDirectory, CoreName: "xray", CorePath: original}
	for _, option := range options {
		option(dataDirectory)
	}
	if len(options) != 0 {
		// A marker written for a restart is only read when the reconciler is
		// created again, exactly as a real process start would.
		if manager, err = reconcile.New(dataDirectory, "node-update", adapter); err != nil {
			t.Fatal(err)
		}
		manager.RequireSuccessReceipts = false
	}
	worker, err := newCoreUpdates(context.Background(), cfg, "node-update", manager)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	return worker, cfg, manager, original
}

func coreExecutableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func freeUpdatePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// updateArchive builds an official-style zip holding a valid executable that
// differs from the running one, and returns its bytes and archive digest.
func updateArchive(t *testing.T, original string, marker string) ([]byte, string, string) {
	t.Helper()
	payload, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	payload = append(payload, []byte(marker)...)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create(release.CoreBinaryName("xray", runtime.GOOS))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buffer.Bytes())
	binary := sha256.Sum256(payload)
	return buffer.Bytes(), hex.EncodeToString(sum[:]), hex.EncodeToString(binary[:])
}

// updateSource trusts the official asset URL but downloads it from a local TLS
// server, so the request keeps its production URL validation.
func updateSource(t *testing.T, server *httptest.Server) *updater.CoreSource {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	host := "127.0.0.1"
	if parsed, err := url.Parse(server.URL); err == nil {
		host = parsed.Hostname()
	}
	local, err := url.Parse(server.URL + "/Xray-linux-64.zip")
	if err != nil {
		t.Fatal(err)
	}
	return &updater.CoreSource{
		AllowURL: func(component, value string) (*url.URL, string, error) {
			if _, _, err := release.CoreAssetURL(component, value); err != nil {
				return nil, "", err
			}
			return local, "v26.3.27", nil
		},
		AllowRedirectHost: func(candidate string) bool { return candidate == host },
		Roots:             pool,
	}
}

const updateAssetURL = "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip"

func updateRequest(request release.CoreUpdateRequest) *pb.CoreUpdateCommand {
	data, _ := json.Marshal(request)
	return &pb.CoreUpdateCommand{RequestJson: data}
}

// A8.1: an archive whose digest does not match is rejected before extraction.
// Nothing is installed, no history is written, staging is removed and the core
// is never switched.
func TestCoreUpdateChecksumMismatchNeverActivates(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	worker, cfg, manager, original := updateHarness(t, adapter)
	archive, digest, _ := updateArchive(t, original, "candidate")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	defer server.Close()
	worker.source = updateSource(t, server)
	wrong := digest[:len(digest)-1] + "0"
	if wrong == digest {
		wrong = digest[:len(digest)-1] + "1"
	}
	request := release.CoreUpdateRequest{SchemaVersion: 2, ID: "upd_checksum", NodeID: "node-update", Revision: 1, Sequence: 1, Component: "xray", Version: "26.3.27", URL: updateAssetURL, SHA256: wrong}
	if err := worker.submit(updateRequest(request)); err != nil {
		t.Fatal(err)
	}
	result := awaitResult(t, worker)
	if result.Status != "rejected" || result.Code != "checksum_mismatch" || result.InstalledDigest != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if adapter.activate != 0 || manager.CoreUpdateHeld() {
		t.Fatalf("core switched for a rejected archive: %d %v", adapter.activate, manager.CoreUpdateHeld())
	}
	history, err := openCoreHistory(cfg, "node-update")
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if state, ok, err := history.InstallationRequest("upd_checksum"); err != nil || ok {
		t.Fatalf("rejected update wrote installation state: %+v %v", state, err)
	}
	installed, err := os.ReadDir(filepath.Join(cfg.DataDirectory, "releases", "installed"))
	if err != nil && !errors.Is(err, os.ErrNotExist) || len(installed) != 0 {
		t.Fatalf("rejected update installed an image: %v %v", installed, err)
	}
	if entries, err := os.ReadDir(filepath.Join(cfg.DataDirectory, "releases", "core-staging")); err != nil && !errors.Is(err, os.ErrNotExist) || len(entries) != 0 {
		t.Fatalf("staging not removed: %v %v", entries, err)
	}
}

// A8.2 (i): the downloaded binary reports another version, so the switch never
// happens and the previous image stays current.
func TestCoreUpdateVersionMismatchKeepsCurrentImage(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "1.0.0"}
	worker, cfg, manager, original := updateHarness(t, adapter)
	archive, digest, _ := updateArchive(t, original, "candidate")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	defer server.Close()
	worker.source = updateSource(t, server)
	request := release.CoreUpdateRequest{SchemaVersion: 2, ID: "upd_version", NodeID: "node-update", Revision: 1, Sequence: 1, Component: "xray", Version: "26.3.27", URL: updateAssetURL, SHA256: digest}
	if err := worker.submit(updateRequest(request)); err != nil {
		t.Fatal(err)
	}
	result := awaitResult(t, worker)
	if result.Status != "rejected" || result.Code != "version_mismatch" || result.InstalledDigest != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if adapter.activate != 0 || adapter.selected != original || manager.CoreUpdateHeld() {
		t.Fatalf("core switched for a version mismatch: %d %q %v", adapter.activate, adapter.selected, manager.CoreUpdateHeld())
	}
	history, err := openCoreHistory(cfg, "node-update")
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	state, ok, err := history.Installation("xray")
	if err != nil {
		t.Fatal(err)
	}
	if ok && (state.Phase != "ready" || state.Current.Sequence != 0 || state.Target != nil) {
		t.Fatalf("history changed for a version mismatch: %+v", state)
	}
	if _, recorded, err := history.InstallationRequest("upd_version"); err != nil || recorded {
		t.Fatalf("version mismatch wrote installation state: %v", err)
	}
}

// A8.2 (ii): the new core starts but fails its health check, so the previous
// image is restored and the installation is recorded as rolled back.
func TestCoreUpdateHealthFailureRollsBackToPreviousImage(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	worker, cfg, manager, original := updateHarness(t, adapter)
	archive, digest, binaryDigest := updateArchive(t, original, "candidate")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	defer server.Close()
	worker.source = updateSource(t, server)
	adapter.badPath = filepath.Join(cfg.DataDirectory, "releases", "installed", installedImageName(binaryDigest))
	request := release.CoreUpdateRequest{SchemaVersion: 2, ID: "upd_health", NodeID: "node-update", Revision: 1, Sequence: 1, Component: "xray", Version: "26.3.27", URL: updateAssetURL, SHA256: digest}
	if err := worker.submit(updateRequest(request)); err != nil {
		t.Fatal(err)
	}
	result := awaitResult(t, worker)
	if result.Status != "rolled_back" || result.Code != "rolled_back" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if adapter.selected == adapter.badPath {
		t.Fatalf("previous image is not active: %q", adapter.selected)
	}
	selected, err := os.ReadFile(adapter.selected)
	originalBytes, readErr := os.ReadFile(original)
	if err != nil || readErr != nil || !bytes.Equal(selected, originalBytes) {
		t.Fatalf("active image is not the retained one: %q %v", adapter.selected, err)
	}
	if manager.CoreUpdateHeld() {
		t.Fatal("hold not released after a successful rollback")
	}
	history, err := openCoreHistory(cfg, "node-update")
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	state, ok, err := history.Installation("xray")
	if err != nil || !ok || state.Phase != "rolled_back" || state.Current.Sequence != 0 || state.Previous != nil {
		t.Fatalf("history does not record the rollback: %+v %v", state, err)
	}
}

// installedImageName mirrors the Agent's local image naming.
func installedImageName(binaryDigest string) string {
	name := "xray-" + binaryDigest + ".bin"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func awaitResult(t *testing.T, worker *coreUpdates) release.UpdateResult {
	t.Helper()
	select {
	case payload := <-worker.results:
		result, err := release.DecodeCoreUpdateResult(payload.ResultJson)
		if err != nil {
			t.Fatalf("invalid core result: %v", err)
		}
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("core update produced no result")
	}
	return release.UpdateResult{}
}

// A node whose core came from install.sh has no release history. Its first
// core update creates one, records the running core as the baseline and
// switches to the downloaded image.
func TestCoreUpdateWithoutHistoryCreatesItAndSwitches(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	worker, cfg, _, original := updateHarness(t, adapter, func(dataDirectory string) {
		for _, path := range []string{filepath.Join(dataDirectory, "release-history"), filepath.Join(dataDirectory, "identity", "release-checkpoint.enc")} {
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
		}
	})
	archive, digest, _ := updateArchive(t, original, "first-managed")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	defer server.Close()
	worker.source = updateSource(t, server)
	request := release.CoreUpdateRequest{SchemaVersion: 2, ID: "upd_first", NodeID: "node-update", Revision: 1, Sequence: 1, Component: "xray", Version: "26.3.27", URL: updateAssetURL, SHA256: digest}
	if err := worker.submit(updateRequest(request)); err != nil {
		t.Fatal(err)
	}
	if result := awaitResult(t, worker); result.Status != "success" {
		t.Fatalf("first core update without history: %+v", result)
	}
	if adapter.selected == original {
		t.Fatal("core not switched to the downloaded image")
	}
	history, err := openCoreHistory(cfg, "node-update")
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if state, ok, err := history.Installation("xray"); err != nil || !ok || state.Phase != "committed" {
		t.Fatalf("installation not recorded: %+v %v %v", state, ok, err)
	}
}

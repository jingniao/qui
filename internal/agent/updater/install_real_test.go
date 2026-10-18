package updater_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/singbox"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/release"
)

func updatePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
func updateState(port int) desiredstate.State {
	return desiredstate.State{SchemaVersion: 1, NodeID: "update-node", Revision: 1, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: "user", Enabled: true}}, Credentials: []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "http", Username: "update-user", Secret: "update-device-password"}}, Inbounds: []desiredstate.Inbound{{ID: "entry", Protocol: "http", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"credential"}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
}
func updateAdapter(component, path, directory string) core.BinaryRuntime {
	if component == "xray" {
		return &xray.Adapter{Path: path, Directory: directory, NodeID: "update-node"}
	}
	return &singbox.Adapter{Path: path, Directory: directory, NodeID: "update-node"}
}
func updateHistory(t *testing.T, root string) *updater.History {
	t.Helper()
	key, err := os.ReadFile(filepath.Join(root, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := updater.CreateHistory(updater.HistoryOptions{Directory: filepath.Join(root, "release-history"), Checkpoint: filepath.Join(root, "identity", "release-checkpoint.enc"), NodeID: "update-node", Key: key})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}
func updateCandidate(t *testing.T, h *updater.History, root, component, path, version string, sequence int64) (updater.Candidate, release.Policy) {
	t.Helper()
	artifact, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(artifact)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	signed, err := release.Sign(release.Manifest{SchemaVersion: 1, Component: component, Version: version, OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: sequence, Bytes: int64(len(artifact)), SHA256: hex.EncodeToString(hash[:]), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}, private)
	if err != nil {
		t.Fatal(err)
	}
	policy := release.Policy{Component: component, OS: runtime.GOOS, Architecture: runtime.GOARCH, Now: now, Keys: map[string]release.TrustedKey{signed.KeyID: {PublicKey: public, Components: []string{component}}}}
	candidate, err := updater.Stage(context.Background(), filepath.Join(root, "candidates"), signed, policy, bytes.NewReader(artifact))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Accept(context.Background(), signed, policy); err != nil {
		t.Fatal(err)
	}
	return candidate, policy
}
func updateRequest(t *testing.T, state desiredstate.State, target string) []byte {
	t.Helper()
	proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(state.Inbounds[0].Port))
	proxyURL.User = url.UserPassword(state.Credentials[0].Username, state.Credentials[0].Secret)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type healthFailureRuntime struct {
	core.BinaryRuntime
	failNext bool
}

type checkpointFaultDriver struct {
	updater.Driver
	checkpoint string
	injected   bool
}

func (d *checkpointFaultDriver) Activate(ctx context.Context, path string) error {
	if err := d.Driver.Activate(ctx, path); err != nil {
		return err
	}
	if !d.injected {
		d.injected = true
		return os.Mkdir(d.checkpoint+".next", 0700)
	}
	return nil
}

func TestRealCoreCheckpointFailureCannotBeReopenedBySupervisorAndCanRecover(t *testing.T) {
	path := os.Getenv("XRAY_BINARY")
	if path == "" {
		t.Skip("set Xray for actual failed installation recovery")
	}
	ctx := context.Background()
	root := t.TempDir()
	adapter := updateAdapter("xray", path, filepath.Join(root, "runtime"))
	defer adapter.Close()
	manager, err := reconcile.New(root, "update-node", adapter)
	if err != nil {
		t.Fatal(err)
	}
	state := updateState(updatePort(t))
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	h := updateHistory(t, root)
	capabilities, err := adapter.DetectCapabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidate, policy := updateCandidate(t, h, root, "xray", path, capabilities.CoreVersion, 1)
	installer := &updater.Installer{History: h, Directory: filepath.Join(root, "releases", "installed")}
	checkpoint := filepath.Join(root, "identity", "release-checkpoint.enc")
	err = manager.WithCoreUpdate(ctx, 1, func(lease *reconcile.CoreUpdateLease) error {
		driver := &checkpointFaultDriver{Driver: lease, checkpoint: checkpoint}
		_, err := installer.Apply(ctx, driver, "checkpoint-fault", 1, candidate, policy)
		return err
	})
	if err == nil {
		t.Fatal("actual checkpoint fault reported update success")
	}
	if err := manager.Restore(ctx); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("supervisor restore bypassed interrupted installation", err)
	}
	manager, err = reconcile.New(root, "update-node", adapter)
	if err != nil || !manager.CoreUpdateHeld() {
		t.Fatal("restart lost interrupted installation hold", err)
	}
	if err := manager.Restore(ctx); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("restarted supervisor reopened interrupted installation", err)
	}
	if _, err := manager.Refresh(ctx); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("time refresh bypassed interrupted installation", err)
	}
	if _, err := manager.Apply(ctx, state); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("publication bypassed interrupted installation", err)
	}
	if _, _, err := manager.RollbackCommand(ctx, "old-rollback", 1); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("configuration rollback bypassed interrupted installation", err)
	}
	if connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(state.Inbounds[0].Port)), time.Second); err == nil {
		connection.Close()
		t.Fatal("unknown installation kept listener open")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(checkpoint + ".next"); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(root, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := updater.OpenHistory(updater.HistoryOptions{Directory: filepath.Join(root, "release-history"), Checkpoint: checkpoint, NodeID: state.NodeID, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	installer.History = reopened
	if err := manager.WithCoreUpdate(ctx, 1, func(lease *reconcile.CoreUpdateLease) error { _, err := installer.Recover(ctx, lease); return err }); err != nil {
		t.Fatal("confirmed original core could not recover", err)
	}
	s, _, err := reopened.Installation("xray")
	if err != nil || s.Phase != "rolled_back" || s.Current.Sequence != 0 {
		t.Fatal(s, err)
	}
	if err := manager.Restore(ctx); err != nil {
		t.Fatal("confirmed recovery did not release runtime hold", err)
	}
}

func TestRealCommittedImageReceiptHoldSurvivesRestartAndReconcilesExactImage(t *testing.T) {
	path := os.Getenv("XRAY_BINARY")
	if path == "" {
		t.Skip("set actual Xray")
	}
	ctx := context.Background()
	root := t.TempDir()
	state := updateState(updatePort(t))
	adapter := updateAdapter("xray", path, filepath.Join(root, "runtime"))
	defer adapter.Close()
	manager, err := reconcile.New(root, state.NodeID, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	h := updateHistory(t, root)
	caps, err := adapter.DetectCapabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidate, policy := updateCandidate(t, h, root, "xray", path, caps.CoreVersion, 1)
	installer := &updater.Installer{History: h, Directory: filepath.Join(root, "releases", "installed")}
	if err := manager.WithCoreUpdate(ctx, 1, func(lease *reconcile.CoreUpdateLease) error {
		_, err := installer.Apply(ctx, lease, "committed-before-receipt", 1, candidate, policy)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Receipt persistence fails after the authoritative native install commit.
	if err := manager.WithCoreUpdate(ctx, 1, func(lease *reconcile.CoreUpdateLease) error { return errors.Join(lease.Hold(), lease.Stop(ctx)) }); err != nil {
		t.Fatal(err)
	}
	restarted, err := reconcile.New(root, state.NodeID, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Restore(ctx); !errors.Is(err, core.ErrReleaseRecovery) {
		t.Fatal("restart reopened committed but receipt-held core", err)
	}
	if err := restarted.WithCoreUpdate(ctx, 1, func(lease *reconcile.CoreUpdateLease) error {
		_, err := installer.ReconcileCommitted(ctx, lease, "different-request")
		return err
	}); !errors.Is(err, updater.ErrInstallation) {
		t.Fatal("unrelated request released committed hold", err)
	}
	if err := restarted.WithCoreUpdate(ctx, 1, func(lease *reconcile.CoreUpdateLease) error {
		_, err := installer.ReconcileCommitted(ctx, lease, "committed-before-receipt")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if restarted.CoreUpdateHeld() {
		t.Fatal("exact recorded healthy image did not release hold")
	}
	if err := restarted.Restore(ctx); err != nil {
		t.Fatal(err)
	}
}

func (r *healthFailureRuntime) Health(ctx context.Context) (core.HealthStatus, error) {
	s, err := r.BinaryRuntime.Health(ctx)
	if err == nil && r.failNext {
		r.failNext = false
		s.Healthy = false
		return s, errors.New("injected post-start native health failure")
	}
	return s, err
}

func TestRealCoreSignedInstallRollbackAndColdRestart(t *testing.T) {
	for _, component := range []string{"xray", "sing-box"} {
		for _, failHealth := range []bool{false, true} {
			t.Run(component+map[bool]string{false: "-success", true: "-health-rollback"}[failHealth], func(t *testing.T) {
				path := os.Getenv("XRAY_BINARY")
				if component == "sing-box" {
					path = os.Getenv("SINGBOX_BINARY")
				}
				if path == "" {
					t.Skip("set core binary for real update integration")
				}
				ctx := context.Background()
				root := t.TempDir()
				adapter := &healthFailureRuntime{BinaryRuntime: updateAdapter(component, path, filepath.Join(root, "runtime"))}
				defer adapter.Close()
				manager, err := reconcile.New(root, "update-node", adapter)
				if err != nil {
					t.Fatal(err)
				}
				state := updateState(updatePort(t))
				if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
					t.Fatal(result, err)
				}
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "real-updated-core") }))
				defer origin.Close()
				if string(updateRequest(t, state, origin.URL)) != "real-updated-core" {
					t.Fatal("original core did not forward")
				}
				originalManifest, err := os.ReadFile(filepath.Join(root, "current.json"))
				if err != nil {
					t.Fatal(err)
				}
				capabilities, err := adapter.DetectCapabilities(ctx)
				if err != nil {
					t.Fatal(err)
				}
				h := updateHistory(t, root)
				candidate, policy := updateCandidate(t, h, root, component, path, capabilities.CoreVersion, 1)
				installer := &updater.Installer{History: h, Directory: filepath.Join(root, "images")}
				if err := manager.WithCoreUpdate(ctx, 1, func(lease *reconcile.CoreUpdateLease) error {
					_, err := installer.Apply(ctx, lease, "wrong-config-scope", 2, candidate, policy)
					return err
				}); !errors.Is(err, updater.ErrInstallation) {
					t.Fatal("installation accepted another configuration baseline", err)
				}
				adapter.failNext = failHealth
				var result updater.Installation
				err = manager.WithCoreUpdate(ctx, 1, func(lease *reconcile.CoreUpdateLease) error {
					var err error
					result, err = installer.Apply(ctx, lease, "actual-update", 1, candidate, policy)
					return err
				})
				if failHealth {
					if err == nil || result.Phase != "rolled_back" || result.Current.Sequence != 0 {
						t.Fatal(result, err)
					}
				} else if err != nil || result.Phase != "committed" || result.Current.Sequence != 1 {
					t.Fatal(result, err)
				}
				if manager.Current() != 1 || string(updateRequest(t, state, origin.URL)) != "real-updated-core" {
					t.Fatal("update changed business revision or stopped forwarding")
				}
				after, err := os.ReadFile(filepath.Join(root, "current.json"))
				if err != nil || !bytes.Equal(after, originalManifest) {
					t.Fatal("core update rewrote desired configuration", err)
				}
				imagePath, err := updater.ResolveImage(ctx, installer.Directory, component, result.Current)
				if err != nil || adapter.ExecutablePath() != imagePath {
					t.Fatal("actual executable does not match durable state", err)
				}
				if err := adapter.Close(); err != nil {
					t.Fatal(err)
				}
				restarted := updateAdapter(component, imagePath, filepath.Join(root, "runtime"))
				defer restarted.Close()
				restored, err := reconcile.New(root, "update-node", restarted)
				if err != nil {
					t.Fatal(err)
				}
				if err := restored.Restore(ctx); err != nil {
					t.Fatal("installed core cold restart", err)
				}
				if string(updateRequest(t, state, origin.URL)) != "real-updated-core" {
					t.Fatal("installed state did not survive restart")
				}
			})
		}
	}
}

package updater_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/release"
)

const realCoreAssetURL = "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip"

// TestRealCoreDirectURLInstallProxies verifies the whole direct-URL path with
// the official Xray binary: archive download, streamed digest verification,
// extraction, version gate, supervised switch and working proxying.
func TestRealCoreDirectURLInstallProxies(t *testing.T) {
	path := os.Getenv("XRAY_BINARY")
	if path == "" {
		t.Skip("set XRAY_BINARY for the real direct-URL core install")
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
	history := updateHistory(t, root)
	capabilities, err := adapter.DetectCapabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The candidate is the same official binary with a distinguishing suffix,
	// so the installed digest provably differs from the retained one.
	binary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	candidate := append(append([]byte(nil), binary...), []byte("proxycontrol-core-update")...)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create(release.CoreBinaryName("xray", runtime.GOOS))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(candidate); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive.Bytes()) }))
	defer server.Close()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	local, err := url.Parse(server.URL + "/Xray-linux-64.zip")
	if err != nil {
		t.Fatal(err)
	}
	source := &updater.CoreSource{AllowURL: func(component, value string) (*url.URL, string, error) {
		if _, _, err := release.CoreAssetURL(component, value); err != nil {
			return nil, "", err
		}
		return local, "v26.3.27", nil
	}, Roots: pool}
	sum := sha256.Sum256(archive.Bytes())
	extracted, err := source.FetchCore(ctx, filepath.Join(root, "releases", "core-staging"), "xray", realCoreAssetURL, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	image, err := updater.InspectImage(ctx, "xray", extracted, capabilities.CoreVersion)
	if err != nil {
		t.Fatal(err)
	}
	image.Sequence = 1
	installer := &updater.Installer{History: history, Directory: filepath.Join(root, "releases", "installed")}
	var installed updater.Installation
	err = manager.WithCoreUpdate(ctx, 1, func(lease *reconcile.CoreUpdateLease) error {
		version, err := lease.DetectVersion(ctx, extracted)
		if err != nil {
			return err
		}
		if version != capabilities.CoreVersion {
			t.Fatalf("candidate reports %q, expected %q", version, capabilities.CoreVersion)
		}
		installed, err = installer.ApplyImage(ctx, lease, "upd_real", 1, image, extracted)
		return err
	})
	if err != nil || installed.Phase != "committed" || installed.Current.Sequence != 1 {
		t.Fatalf("real core install failed: %+v %v", installed, err)
	}
	installedSum := sha256.Sum256(candidate)
	if installed.Current.SHA256 != hex.EncodeToString(installedSum[:]) {
		t.Fatalf("installed digest mismatch: %q", installed.Current.SHA256)
	}
	if manager.CoreUpdateHeld() {
		t.Fatal("hold not released after a successful install")
	}
	live, err := adapter.DetectCapabilities(ctx)
	if err != nil || live.CoreVersion != capabilities.CoreVersion {
		t.Fatalf("core version after update: %+v %v", live, err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("core-update-proxy-ok")) }))
	defer target.Close()
	if body := updateRequest(t, state, target.URL); string(body) != "core-update-proxy-ok" {
		t.Fatalf("proxying after the update returned %q", body)
	}
	if _, err := os.Stat(extracted); err != nil {
		t.Fatalf("staging was not cleaned by the caller: %v", err)
	}
	_ = io.Discard
	_ = time.Second
}

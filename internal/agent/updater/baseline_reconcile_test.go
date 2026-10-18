package updater

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/pkg/contracts/release"
)

type baselineRecoverySetup struct {
	options   HistoryOptions
	history   *History
	request   release.UpdateRequest
	original  release.UpdateRequest
	signed    release.Signed
	policy    release.Policy
	image     Image
	directory string
	path      string
}

// baselineRecoverySetupFixture builds a stopped supervised Agent whose
// confirmed image is the real local program and whose refused install is
// recorded as an authenticated pre-switch rejection.
func baselineRecoverySetupFixture(t *testing.T) baselineRecoverySetup {
	t.Helper()
	ctx := context.Background()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := InspectImage(ctx, "proxy-agent", executable, "1.0")
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "images")
	path, err := MaterializeImage(ctx, directory, executable, "proxy-agent", image)
	if err != nil {
		t.Fatal(err)
	}
	options, signed, policy, _ := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	if err := h.Accept(ctx, signed, policy); err != nil {
		t.Fatal(err)
	}
	if err := h.BootstrapImage(ctx, "proxy-agent", image); err != nil {
		t.Fatal(err)
	}
	original := release.UpdateRequest{SchemaVersion: 1, ID: "upd_refused", NodeID: options.NodeID, Revision: 1, Operation: "install", Release: signed}
	request := original
	request.ID = "upd_restore_baseline"
	request.Operation = "recover"
	request.OriginalID = original.ID
	rejection := release.UpdateResult{ID: original.ID, NodeID: original.NodeID, Revision: original.Revision, TargetDigest: signed.Manifest.SHA256, Status: "rejected", Code: "preflight_failed"}
	record := BaselineRecovery{Request: request, Original: original, Rejection: rejection, Baseline: image, InstallationGeneration: 0, Phase: "prepared"}
	if _, err := h.BeginBaselineRecovery(ctx, record); err != nil {
		t.Fatal(err)
	}
	return baselineRecoverySetup{options: options, history: h, request: request, original: original, signed: signed, policy: policy, image: image, directory: directory, path: path}
}

// The agent update recovery path reconciles a recorded pre-switch refusal only
// through a proof minted from the original authenticated history, and the
// refused original command can never be installed afterwards.
func TestBaselineReconcileRestoresConfirmedImageThroughItsProof(t *testing.T) {
	ctx := context.Background()
	f := baselineRecoverySetupFixture(t)
	if _, pending, err := f.history.PendingBaselineRecovery("proxy-agent"); err != nil || !pending {
		t.Fatal("prepared baseline recovery is not pending for the next start", pending, err)
	}
	driver := &retainedDriver{node: f.options.NodeID, path: f.path}
	restored, err := (&Installer{History: f.history, Directory: f.directory}).ReconcileBaseline(ctx, driver, f.request.ID)
	if err != nil || restored.Phase != "recovered" {
		t.Fatal("baseline reconciliation failed", restored, err)
	}
	if !driver.held || !driver.activated || !driver.released || driver.stopped {
		t.Fatal("recovery did not hand the confirmed image back to the worker", driver)
	}
	if _, pending, err := f.history.PendingBaselineRecovery("proxy-agent"); err != nil || pending {
		t.Fatal("completed recovery is still pending", pending, err)
	}
	if _, err := f.history.BeginInstallation(ctx, f.original.ID, f.original.Revision, f.signed, f.policy); err == nil {
		t.Fatal("refused original install was reopened")
	}
	if current, ok, err := f.history.Installation("proxy-agent"); err != nil || !ok || current.Current != f.image || current.Generation != 0 {
		t.Fatal("recovery changed the confirmed image", current, err)
	}
}

func TestBaselineReconcileWithoutConfirmedImageNeverTouchesTheWorker(t *testing.T) {
	ctx := context.Background()
	f := baselineRecoverySetupFixture(t)
	if err := os.Rename(f.path, f.path+".missing"); err != nil {
		t.Fatal(err)
	}
	driver := &retainedDriver{node: f.options.NodeID, path: f.path}
	if _, err := (&Installer{History: f.history, Directory: f.directory}).ReconcileBaseline(ctx, driver, f.request.ID); err == nil {
		t.Fatal("recovery without the confirmed image was accepted")
	}
	if driver.held || driver.activated || driver.released || driver.stopped {
		t.Fatal("unbound recovery touched the worker", driver)
	}
	if _, pending, err := f.history.PendingBaselineRecovery("proxy-agent"); err != nil || !pending {
		t.Fatal("failed recovery lost the pending baseline", pending, err)
	}
}

package updater

import (
	"context"
	"errors"
	"os"
	"proxycontrol/pkg/contracts/release"
	"testing"
)

func baselineJournalFixture(t *testing.T, component ...string) (HistoryOptions, *History, release.Signed, release.Policy, BaselineRecovery) {
	t.Helper()
	options, signed, policy, private := historyFixture(t)
	m := signed.Manifest
	m.Component = "xray"
	if len(component) > 0 {
		m.Component = component[0]
	}
	signed, err := release.Sign(m, private)
	if err != nil {
		t.Fatal(err)
	}
	policy.Component = m.Component
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	if err := h.Accept(context.Background(), signed, policy); err != nil {
		t.Fatal(err)
	}
	image := imageOf(m)
	image.Sequence = 0
	if err := h.BootstrapImage(context.Background(), m.Component, image); err != nil {
		t.Fatal(err)
	}
	original := release.UpdateRequest{SchemaVersion: 1, ID: "upd_refused", NodeID: options.NodeID, Revision: 4, Operation: "install", Release: signed}
	request := original
	request.ID = "upd_restore_baseline"
	request.Operation = "recover"
	request.OriginalID = original.ID
	rejection := release.UpdateResult{ID: original.ID, NodeID: original.NodeID, Revision: 4, TargetDigest: m.SHA256, Status: "rejected", Code: "preflight_failed"}
	return options, h, signed, policy, BaselineRecovery{Request: request, Original: original, Rejection: rejection, Baseline: image, InstallationGeneration: 0, Phase: "prepared"}
}

func TestBaselineRecoveryJournalReplaysRefusalBarrierAndPreservesHighWater(t *testing.T) {
	options, h, signed, policy, record := baselineJournalFixture(t)
	ctx := context.Background()
	if _, err := h.BeginBaselineRecovery(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err := h.BeginInstallation(ctx, "other-update", 4, signed, policy); !errors.Is(err, ErrUpdatePending) {
		t.Fatal("pending baseline restoration bypassed", err)
	}
	if _, err := h.finishBaselineRecovery(ctx, record.Request.ID, "recovered"); err != nil {
		t.Fatal(err)
	}
	h.Close()
	reopened, err := OpenHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.BeginInstallation(ctx, record.Original.ID, 4, signed, policy); !errors.Is(err, ErrInstallation) {
		t.Fatal("refusal nonce lost across restart", err)
	}
	if reopened.highest["xray"] != signed.Manifest {
		t.Fatal("baseline restore reset accepted sequence")
	}
	if r, ok, err := reopened.BaselineRecovery(record.Request.ID); err != nil || !ok || r.Phase != "recovered" || r.Rejection != record.Rejection {
		t.Fatal(r, err)
	}
	// A distinct new command remains eligible; original-ID closure is precise.
	if _, err := reopened.BeginInstallation(ctx, "different-approved-update", 5, signed, policy); err != nil {
		t.Fatal("baseline restore permanently blocked future updates", err)
	}
}

func TestBaselineRecoveryRejectsFalseRejectionAndWrongIdentity(t *testing.T) {
	for _, mode := range []string{"failure-not-rejection", "installed-claim", "wrong-original", "changed-signature", "wrong-baseline", "wrong-generation"} {
		t.Run(mode, func(t *testing.T) {
			_, h, _, _, record := baselineJournalFixture(t)
			switch mode {
			case "failure-not-rejection":
				record.Rejection.Status = "failed_closed"
				record.Rejection.Code = "installation_failed"
			case "installed-claim":
				record.Rejection.InstalledDigest = record.Baseline.SHA256
				record.Rejection.InstalledVersion = record.Baseline.Version
			case "wrong-original":
				record.Request.OriginalID = "upd_foreign"
			case "changed-signature":
				record.Original.Release.Signature = "altered"
			case "wrong-baseline":
				record.Baseline.Bytes++
			case "wrong-generation":
				record.InstallationGeneration++
			}
			before := h.head.Number
			if _, err := h.BeginBaselineRecovery(context.Background(), record); err == nil {
				t.Fatal("unproven no-switch claim accepted", mode)
			}
			if h.head.Number != before {
				t.Fatal("invalid proof wrote an event")
			}
		})
	}
}

func TestBaselineRecoveryAuthenticatedUncheckpointedTailRemainsPending(t *testing.T) {
	options, h, _, _, record := baselineJournalFixture(t)
	data, err := h.encode(historyEvent{Kind: "release-baseline-recovery-v1", NodeID: options.NodeID, Number: h.head.Number + 1, Previous: h.head.Digest, BaselineRecovery: &record})
	if err != nil {
		t.Fatal(err)
	}
	if err := exclusiveRoot(h.root, eventName(h.head.Number+1), data); err != nil {
		t.Fatal(err)
	}
	h.Close()
	reopened, err := OpenHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if r, active, err := reopened.PendingBaselineRecovery("xray"); err != nil || !active || r.Request != record.Request {
		t.Fatal("uncheckpointed tail was not retained as unresolved", r, active, err)
	}
	if err := os.Remove(options.Checkpoint); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.BaselineRecovery(record.Request.ID); err == nil {
		t.Fatal("missing independent checkpoint ignored")
	}
}

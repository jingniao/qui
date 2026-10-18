package agenthost

import (
	"crypto/ed25519"
	"crypto/rand"
	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/release"
	"strings"
	"testing"
	"time"
)

func TestAgentRecoveryRequiresOriginalFailedExecutionAndExactInstallation(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	signed, err := release.Sign(release.Manifest{SchemaVersion: 1, Component: "proxy-agent", OS: "windows", Architecture: "amd64", Version: "2.0", Sequence: 1, Bytes: 100, SHA256: strings.Repeat("a", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}, key)
	if err != nil {
		t.Fatal(err)
	}
	original := release.AgentUpdateRequest{SchemaVersion: 1, ID: "original", NodeID: "node", Revision: 1, Operation: "install", Release: signed}
	request := original
	request.ID = "recovery"
	request.Operation = "recover"
	request.OriginalID = original.ID
	failure := release.UpdateResult{ID: original.ID, NodeID: original.NodeID, Revision: 1, TargetDigest: signed.Manifest.SHA256, Status: "failed_closed", Code: "installation_failed"}
	job := agentjobs.Job{Request: original, Started: true, Result: &failure}
	target := updater.Image{SHA256: signed.Manifest.SHA256, Bytes: 100, Version: "2.0", Sequence: 1}
	current := updater.Installation{Component: "proxy-agent", RequestID: original.ID, Revision: 1, Phase: "failed_closed", Target: &target}
	if err := recoveryScope(request, job, current); err != nil {
		t.Fatal(err)
	}
	retainedFailure := job
	retainedFailure.Result = &release.UpdateResult{ID: original.ID, NodeID: original.NodeID, Revision: original.Revision, TargetDigest: signed.Manifest.SHA256, Status: "failed_closed", Code: "installation_failed", InstalledDigest: strings.Repeat("b", 64), InstalledVersion: "1.0", InstalledSequence: 0}
	retainedCurrent := updater.Installation{Component: "proxy-agent", Phase: "ready", Current: updater.Image{SHA256: strings.Repeat("b", 64), Bytes: 90, Version: "1.0", Sequence: 0}}
	if err := recoveryScope(request, retainedFailure, retainedCurrent); err != nil {
		t.Fatal("started failure without installation event must bind to retained baseline", err)
	}
	retainedCurrent.Current.Version = "tampered"
	if recoveryScope(request, retainedFailure, retainedCurrent) == nil {
		t.Fatal("retained baseline recovery accepted a mismatched image")
	}
	for _, phase := range []string{"committed", "rolled_back"} {
		prior := updater.Installation{Component: "proxy-agent", Phase: phase, RequestID: "previous-upgrade", Revision: 1, Current: updater.Image{SHA256: strings.Repeat("b", 64), Bytes: 90, Version: "1.0", Sequence: 0}}
		if err := recoveryScope(request, retainedFailure, prior); err != nil {
			t.Fatal("confirmed previous installation cannot recover unrecorded stop", phase, err)
		}
		prior.Current.Sequence++
		if recoveryScope(request, retainedFailure, prior) == nil {
			t.Fatal("different confirmed sequence accepted", phase)
		}
	}
	successful := job
	result := release.UpdateResult{ID: original.ID, NodeID: original.NodeID, Revision: original.Revision, TargetDigest: signed.Manifest.SHA256, Status: "success", Code: "success", InstalledDigest: signed.Manifest.SHA256, InstalledVersion: signed.Manifest.Version, InstalledSequence: signed.Manifest.Sequence}
	successful.Result = &result
	committed := current
	committed.Phase = "committed"
	committed.Current = target
	if err := recoveryScope(request, successful, committed); err != nil {
		t.Fatal("confirmed local result cannot reconcile unconfirmed central task", err)
	}
	rejected := job
	rejection := failure
	rejection.Status = "rejected"
	rejection.Code = "trust_failed"
	rejected.Result = &rejection
	rejected.Started = false
	baseline := current
	baseline.Phase = "ready"
	baseline.RequestID = ""
	baseline.Target = nil
	if err := recoveryScope(request, rejected, baseline); err != nil {
		t.Fatal("authenticated pre-switch refusal rejected", err)
	}
	rejected.Started = true
	if recoveryScope(request, rejected, baseline) == nil {
		t.Fatal("started request treated as pre-switch refusal")
	}
	for _, mode := range []string{"unstarted", "missing_result", "wrong_node", "wrong_revision", "other_install", "missing_target", "wrong_target", "wrong_phase", "different_signature"} {
		t.Run(mode, func(t *testing.T) {
			r, j, c := request, job, current
			switch mode {
			case "unstarted":
				j.Started = false
			case "missing_result":
				j.Result = nil
			case "wrong_node":
				r.NodeID = "other"
			case "wrong_revision":
				r.Revision++
			case "other_install":
				c.RequestID = "other"
			case "missing_target":
				c.Target = nil
			case "wrong_target":
				image := target
				image.Sequence++
				c.Target = &image
			case "wrong_phase":
				c.Phase = "ready"
			case "different_signature":
				r.Release.Signature = "changed"
			}
			if recoveryScope(r, j, c) == nil {
				t.Fatal("unbound recovery accepted")
			}
		})
	}
}

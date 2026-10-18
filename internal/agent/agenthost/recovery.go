package agenthost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/agent/agentprocess"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/release"
)

func retainedInstallation(current updater.Installation) bool {
	if current.Component != "proxy-agent" {
		return false
	}
	if current.Phase == "ready" {
		return current.RequestID == "" && current.Revision == 0 && current.Target == nil
	}
	return current.Phase == "committed" || current.Phase == "rolled_back"
}

func recoveryScope(request release.AgentUpdateRequest, original agentjobs.Job, current updater.Installation) error {
	if request.Validate() != nil || request.Operation != "recover" || original.Request.Validate() != nil || original.Request.Operation != "install" || original.Request.ID != request.OriginalID || original.Request.NodeID != request.NodeID || original.Request.Revision != request.Revision || original.Request.Release != request.Release || original.Result == nil || release.ValidateAgentResult(original.Request, *original.Result) != nil {
		return ErrState
	}
	if original.Result.Status == "rejected" {
		if original.Started || original.Result.InstalledDigest != "" || current.Component != "proxy-agent" || current.Phase != "ready" && current.Phase != "committed" && current.Phase != "rolled_back" {
			return ErrState
		}
		return nil
	}
	if !original.Started {
		return ErrState
	}
	// A worker can stop after the journal Started event but before the
	// installer records switching. In that case the durable history remains
	// the authenticated confirmed image and recovery must bind to that exact
	// image, rather than inventing an installation event.
	if original.Result.Status == "failed_closed" && original.Result.Code == "installation_failed" &&
		retainedInstallation(current) && current.RequestID != original.Request.ID &&
		current.Current.SHA256 == original.Result.InstalledDigest && current.Current.Version == original.Result.InstalledVersion && current.Current.Sequence == original.Result.InstalledSequence {
		return nil
	}
	m := request.Release.Manifest
	if current.Component != "proxy-agent" || current.RequestID != request.OriginalID || current.Revision != request.Revision || current.Target == nil || *current.Target != (updater.Image{SHA256: m.SHA256, Bytes: m.Bytes, Version: m.Version, Sequence: m.Sequence}) {
		return ErrState
	}
	switch current.Phase {
	case "switching", "rolling_back", "failed_closed", "committed", "rolled_back":
		return nil
	}
	return ErrState
}

// recoverJob uses only the authenticated original installation. This path
// never downloads the rejected target or replaces the original job outcome.
func (r *jobRunner) recoverJob(job agentjobs.Job, current updater.Installation) (release.UpdateResult, error) {
	if err := r.refreshRequired(); err != nil {
		return release.UpdateResult{}, err
	}
	request := job.Request
	store, err := agentjobs.Open(jobsOptions(r.directory, r.nodeID, r.key))
	if err != nil {
		return release.UpdateResult{}, err
	}
	original, found := store.Get(request.OriginalID)
	if !found || recoveryScope(request, original, current) != nil {
		store.Close()
		return release.UpdateResult{}, ErrState
	}
	if err := store.Close(); err != nil {
		return release.UpdateResult{}, err
	}
	revision, err := reconcile.InspectCurrent(r.directory, r.nodeID, r.key)
	if err != nil || revision != request.Revision {
		return release.UpdateResult{}, ErrState
	}
	images := filepath.Join(r.directory, "releases", "agent-installed")
	driver, err := agentprocess.NewDriver(r.ctx, images, r.configPath, r.nodeID, revision, current.Current, nil, r.required, os.Stderr)
	if err != nil {
		return jobResult(request, "failed_closed", "recovery_failed", current.Current), nil
	}
	driver.EnableAgentRecovery()
	installer := &updater.Installer{History: r.history, Directory: images}
	baseline := original.Result.Status == "rejected"
	_, originalInstalled, err := r.history.InstallationRequest(original.Request.ID)
	if err != nil {
		return release.UpdateResult{}, err
	}
	retainedBaseline := original.Result.Status == "failed_closed" && !originalInstalled && retainedInstallation(current)
	if baseline {
		if _, err := installer.PrepareBaselineRecovery(r.ctx, driver, release.UpdateRequest(request), release.UpdateRequest(original.Request), *original.Result); err != nil {
			return release.UpdateResult{}, err
		}
	}
	store, err = agentjobs.Open(jobsOptions(r.directory, r.nodeID, r.key))
	if err != nil {
		return release.UpdateResult{}, err
	}
	startErr := store.MarkStarted(request)
	if err := errors.Join(startErr, store.Close()); err != nil {
		return release.UpdateResult{}, err
	}
	if r.process != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		stopErr := r.process.Stop(stopCtx)
		cancel()
		if !r.process.Stopped() {
			return release.UpdateResult{}, errors.Join(agentprocess.ErrStopUnconfirmed, stopErr)
		}
		r.process = nil
		if stopErr != nil {
			_ = r.restartProtected(current.Current, revision)
			return jobResult(request, "failed_closed", "recovery_failed", current.Current), nil
		}
	}
	owner, err := lifecycle.Try(r.directory)
	if err != nil {
		return release.UpdateResult{}, err
	}
	retained, inspectErr := reconcile.InspectCurrent(r.directory, r.nodeID, r.key)
	identityErr := r.checkStoppedIdentity()
	closeErr := owner.Close()
	if inspectErr != nil || identityErr != nil || closeErr != nil || retained != revision {
		return release.UpdateResult{}, errors.Join(ErrState, inspectErr, identityErr, closeErr)
	}
	var recovered updater.Installation
	if baseline {
		var restored updater.BaselineRecovery
		restored, err = installer.ReconcileBaseline(r.ctx, driver, request.ID)
		recovered = updater.Installation{Phase: "rolled_back", Current: restored.Baseline}
	} else if retainedBaseline {
		recovered, err = installer.ReconcileRetained(r.ctx, driver, current.Current)
		if err == nil {
			// Only the recovery outcome is rolled back. The ready installation
			// baseline remains unchanged in the authenticated history.
			recovered.Phase = "rolled_back"
		}
	} else if current.Phase == "committed" || current.Phase == "rolled_back" {
		recovered, err = installer.ReconcileCommitted(r.ctx, driver, current.RequestID)
	} else {
		recovered, err = installer.Recover(r.ctx, driver)
	}
	r.process = driver.Process()
	if r.process != nil && r.process.Stopping() && !r.process.Stopped() {
		return release.UpdateResult{}, errors.Join(agentprocess.ErrStopUnconfirmed, err)
	}
	if err != nil || r.process == nil {
		// A retained worker may keep the control channel available while its
		// accounting protection closes proxy listeners. It is not recovery success.
		if r.process == nil {
			_ = r.restartProtected(current.Current, revision)
		}
		return jobResult(request, "failed_closed", "recovery_failed", current.Current), nil
	}
	if recovered.Phase == "committed" {
		return jobResult(request, "success", "success", recovered.Current), nil
	}
	if recovered.Phase == "rolled_back" {
		return jobResult(request, "rolled_back", "rolled_back", recovered.Current), nil
	}
	return release.UpdateResult{}, ErrState
}

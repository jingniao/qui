package agenthost

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/configuration"
	"runtime"
	"time"

	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/agent/agentprocess"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/config"
	"proxycontrol/pkg/contracts/agentstorage"
	"proxycontrol/pkg/contracts/release"
)

func jobsOptions(directory, nodeID string, key []byte) agentjobs.Options {
	return agentjobs.Options{Directory: filepath.Join(directory, "agent-jobs"), Checkpoint: filepath.Join(directory, "identity", "agent-jobs-checkpoint.enc"), NodeID: nodeID, Key: key}
}

func openJobs(configPath string) (*agentjobs.Store, config.Agent, error) {
	c, err := configuration.LoadAgent(configPath)
	if err != nil {
		return nil, c, err
	}
	node, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil {
		return nil, c, err
	}
	key, err := readOriginal(filepath.Join(c.DataDirectory, "configuration.key"), 32)
	if err != nil {
		return nil, c, err
	}
	defer clear(key)
	r, err := readRecord(filepath.Join(c.DataDirectory, "identity", markerName), key, node.NodeID)
	if err != nil || !r.Jobs {
		return nil, c, ErrState
	}
	store, err := agentjobs.Open(jobsOptions(c.DataDirectory, node.NodeID, key))
	return store, c, err
}

// QueueUpdate is a local fixed operation. It records a signed request only;
// the supervised parent performs downloads and actual process switching.
func QueueUpdate(configPath, id string, revision int64, signed release.Signed) (release.AgentUpdateRequest, error) {
	store, c, err := openJobs(configPath)
	if err != nil {
		return release.AgentUpdateRequest{}, err
	}
	defer store.Close()
	node, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil {
		return release.AgentUpdateRequest{}, err
	}
	r := release.AgentUpdateRequest{SchemaVersion: 1, ID: id, NodeID: node.NodeID, Revision: revision, Operation: "install", Release: signed}
	if r.Validate() != nil {
		return r, release.ErrUpdate
	}
	keys, err := release.PinnedKeys(c.Updates.Keys)
	if err != nil {
		return r, err
	}
	if err := signed.Verify(release.Policy{Component: "proxy-agent", OS: runtime.GOOS, Architecture: runtime.GOARCH, Now: time.Now().UTC(), Keys: keys}); err != nil {
		return r, err
	}
	return r, store.Queue(r)
}

func UpdateStatus(configPath, id string) (agentjobs.Job, error) {
	if !release.ValidUpdateID(id) {
		return agentjobs.Job{}, release.ErrUpdate
	}
	store, _, err := openJobs(configPath)
	if err != nil {
		return agentjobs.Job{}, err
	}
	defer store.Close()
	job, found := store.Get(id)
	if !found {
		return agentjobs.Job{}, os.ErrNotExist
	}
	return job, nil
}

type jobRunner struct {
	ctx                           context.Context
	configPath, directory, nodeID string
	key                           []byte
	required                      agentstorage.Contract
	reader                        agentstorage.Contract
	history                       *updater.History
	process                       *agentprocess.Process
	fingerprint                   string
	prepared                      *preparedAgent
}

type preparedAgent struct {
	request   release.AgentUpdateRequest
	candidate updater.Candidate
}

func (r *jobRunner) poll() error {
	if r.process != nil && r.process.Stopping() && !r.process.Stopped() {
		return agentprocess.ErrStopUnconfirmed
	}
	if err := r.refreshRequired(); err != nil {
		return err
	}
	o := jobsOptions(r.directory, r.nodeID, r.key)
	fingerprint, err := agentjobs.Fingerprint(o)
	if err != nil {
		return err
	}
	if fingerprint == r.fingerprint {
		return nil
	}
	store, err := agentjobs.Open(o)
	if err != nil {
		return err
	}
	job := store.Pending()
	store.Close()
	if job == nil {
		r.fingerprint = fingerprint
		return nil
	}
	result, err := r.execute(*job)
	if err != nil {
		return err
	}
	store, err = agentjobs.Open(o)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Finish(job.Request, result); err != nil {
		return err
	}
	r.prepared = nil
	return nil
}

func jobResult(request release.AgentUpdateRequest, status, code string, image updater.Image) release.UpdateResult {
	r := release.UpdateResult{ID: request.ID, NodeID: request.NodeID, Revision: request.Revision, TargetDigest: request.Release.Manifest.SHA256, Status: status, Code: code}
	if image.SHA256 != "" {
		r.InstalledDigest = image.SHA256
		r.InstalledVersion = image.Version
		r.InstalledSequence = image.Sequence
	}
	return r
}

func (r *jobRunner) restart(image updater.Image, revision int64) error {
	if r.process != nil {
		return agentprocess.ErrStopUnconfirmed
	}
	if err := r.refreshRequired(); err != nil {
		return err
	}
	images := filepath.Join(r.directory, "releases", "agent-installed")
	probe, err := updater.ProbeAgentImage(r.ctx, images, image, r.required)
	if err != nil {
		return err
	}
	baseline, pending, err := r.history.PendingBaselineRecovery("proxy-agent")
	if err != nil {
		return err
	}
	if pending {
		if baseline.Request.Revision != revision {
			return ErrState
		}
		p, err := agentprocess.StartControlOnly(r.ctx, images, image, r.configPath, r.nodeID, revision, r.required, os.Stderr)
		if p != nil {
			r.process = p
		}
		if err != nil {
			return err
		}
		r.process = p
		return nil
	}
	p, err := agentprocess.StartRetained(r.ctx, images, image, r.configPath, r.nodeID, revision, os.Stderr, probe.Contract.AgentUpdateProtocol == 1, probe.Contract.AgentRecoveryProtocol == 1)
	if p != nil {
		r.process = p
	}
	if err != nil {
		return err
	}
	r.process = p
	return nil
}

func (r *jobRunner) restartProtected(image updater.Image, revision int64) error {
	if err := r.refreshRequired(); err != nil {
		return err
	}
	if r.process != nil {
		return ErrState
	}
	current, found, err := r.history.Installation("proxy-agent")
	if err != nil || !found || current.Current != image {
		return errors.Join(ErrState, err)
	}
	retained, err := reconcile.InspectCurrent(r.directory, r.nodeID, r.key)
	if err != nil || retained != revision {
		return errors.Join(ErrState, err)
	}
	p, err := agentprocess.StartControlOnly(r.ctx, filepath.Join(r.directory, "releases", "agent-installed"), image, r.configPath, r.nodeID, revision, r.required, os.Stderr)
	if p != nil {
		r.process = p
	}
	if err != nil {
		return err
	}
	r.process = p
	return nil
}

func (r *jobRunner) execute(job agentjobs.Job) (release.UpdateResult, error) {
	request := job.Request
	if request.Validate() != nil || request.NodeID != r.nodeID {
		return release.UpdateResult{}, release.ErrUpdate
	}
	current, found, err := r.history.Installation("proxy-agent")
	if err != nil || !found {
		return release.UpdateResult{}, ErrState
	}
	if request.Operation == "recover" {
		return r.recoverJob(job, current)
	}
	if original, found, err := r.history.InstallationRequest(request.ID); err != nil {
		return release.UpdateResult{}, err
	} else if found {
		if !job.Started {
			return jobResult(request, "rejected", "scope_mismatch", updater.Image{}), nil
		}
		m := request.Release.Manifest
		if original.Revision != request.Revision || original.Target == nil || original.Target.SHA256 != m.SHA256 || original.Target.Version != m.Version || original.Target.Sequence != m.Sequence || original.Target.Bytes != m.Bytes {
			return release.UpdateResult{}, ErrState
		}
		switch original.Phase {
		case "committed":
			return jobResult(request, "success", "success", original.Current), nil
		case "rolled_back":
			return jobResult(request, "rolled_back", "rolled_back", original.Current), nil
		case "failed_closed":
			return jobResult(request, "failed_closed", "installation_failed", original.Current), nil
		default:
			return release.UpdateResult{}, ErrState
		}
	}
	if request.Operation != "install" {
		return jobResult(request, "failed_closed", "recovery_failed", current.Current), nil
	}
	if job.Started {
		// The journal binds an execution but its installation is absent.
		// Closing this attempt must not download or execute the candidate again.
		if !retainedInstallation(current) || current.RequestID == request.ID {
			return release.UpdateResult{}, ErrState
		}
		return jobResult(request, "failed_closed", "installation_failed", current.Current), nil
	}
	reject := func(code string) (release.UpdateResult, error) {
		return jobResult(request, "rejected", code, updater.Image{}), nil
	}
	c, err := configuration.LoadAgent(r.configPath)
	if err != nil {
		return reject("trust_failed")
	}
	a, err := os.Stat(c.DataDirectory)
	if err != nil {
		return reject("scope_mismatch")
	}
	b, err := os.Stat(r.directory)
	if err != nil || !os.SameFile(a, b) {
		return reject("scope_mismatch")
	}
	keys, err := release.PinnedKeys(c.Updates.Keys)
	if err != nil {
		return reject("trust_failed")
	}
	policy := release.Policy{Component: "proxy-agent", OS: runtime.GOOS, Architecture: runtime.GOARCH, Now: time.Now().UTC(), Keys: keys}
	if err := r.history.Check(request.Release, policy); err != nil {
		return reject("trust_failed")
	}
	// This live snapshot can reject stale scope without disrupting service.
	// Authorization is checked again under ownership after the worker drains.
	observed, err := reconcile.InspectCurrent(r.directory, r.nodeID, r.key)
	if err != nil || observed != request.Revision {
		return reject("scope_mismatch")
	}
	trust, err := c.Updates.TrustConfig()
	if err != nil {
		return reject("trust_failed")
	}
	downloader, err := updater.NewDownloader(c.Updates.Catalogue, trust)
	if err != nil {
		return reject("download_failed")
	}
	defer downloader.Close()
	downloadCtx, cancelDownload := context.WithCancel(r.ctx)
	watchDone := make(chan struct{})
	workerDone := r.process.Done()
	go func() {
		select {
		case <-workerDone:
			cancelDownload()
		case <-watchDone:
		case <-downloadCtx.Done():
		}
	}()
	var candidate updater.Candidate
	if r.prepared != nil && r.prepared.request == request {
		candidate = r.prepared.candidate
	} else {
		candidate, err = downloader.FetchExpected(downloadCtx, filepath.Join(r.directory, "releases", "agent-candidates"), request.Release.Manifest, policy, r.history)
		if err == nil {
			r.prepared = &preparedAgent{request: request, candidate: candidate}
		}
	}
	close(watchDone)
	cancelDownload()
	if err != nil {
		if r.ctx.Err() != nil {
			return release.UpdateResult{}, r.ctx.Err()
		}
		select {
		case <-r.process.Done():
			return release.UpdateResult{}, errors.New("Agent exited while its update was downloading")
		default:
		}
		return reject("download_failed")
	}
	select {
	case <-r.process.Done():
		return release.UpdateResult{}, errors.New("Agent exited while its update was downloading")
	default:
	}
	// The request binds the original complete signature even if the catalogue
	// later re-signs the same manifest with another authorized key.
	candidate.Signed = request.Release
	images := filepath.Join(r.directory, "releases", "agent-installed")
	if err := r.refreshRequired(); err != nil {
		return release.UpdateResult{}, err
	}
	preflight, err := updater.PreflightAgent(r.ctx, images, candidate, policy, r.required)
	if err != nil || preflight.Contract.ManagedProtocol != 1 || preflight.Contract.AgentUpdateProtocol != 1 {
		return reject("preflight_failed")
	}
	policy.Now = time.Now().UTC()
	if err := request.Release.Verify(policy); err != nil {
		return reject("trust_failed")
	}
	if r.process == nil {
		return release.UpdateResult{}, ErrState
	}
	store, err := agentjobs.Open(jobsOptions(r.directory, r.nodeID, r.key))
	if err != nil {
		return release.UpdateResult{}, fmt.Errorf("Agent execution journal unavailable: %w", err)
	}
	startErr := store.MarkStarted(request)
	closeErr := store.Close()
	if startErr != nil || closeErr != nil {
		return release.UpdateResult{}, errors.Join(startErr, closeErr)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	stopErr := r.process.Stop(stopCtx)
	cancel()
	if !r.process.Stopped() {
		return release.UpdateResult{}, errors.Join(agentprocess.ErrStopUnconfirmed, stopErr)
	}
	r.process = nil
	if r.ctx.Err() != nil {
		return release.UpdateResult{}, r.ctx.Err()
	}
	owner, err := lifecycle.Try(r.directory)
	if err != nil {
		return release.UpdateResult{}, err
	}
	revision, inspectErr := reconcile.InspectCurrent(r.directory, r.nodeID, r.key)
	identityErr := r.checkStoppedIdentity()
	closeErr = owner.Close()
	if inspectErr != nil || identityErr != nil || closeErr != nil {
		return release.UpdateResult{}, errors.Join(inspectErr, identityErr, closeErr)
	}
	if stopErr != nil {
		_ = r.restartProtected(current.Current, revision)
		return jobResult(request, "failed_closed", "installation_failed", current.Current), nil
	}
	if revision != request.Revision {
		if err := r.restart(current.Current, revision); err != nil {
			return jobResult(request, "failed_closed", "installation_failed", current.Current), nil
		}
		return reject("scope_mismatch")
	}
	// The Worker may have activated a format after the download's preview.
	// It is stopped now; re-read the authenticated floor before installation.
	if err := r.refreshRequired(); err != nil {
		_ = r.restartProtected(current.Current, revision)
		return jobResult(request, "failed_closed", "installation_failed", current.Current), nil
	}
	driver, err := agentprocess.NewDriver(r.ctx, images, r.configPath, r.nodeID, revision, current.Current, &preflight.Image, r.required, os.Stderr)
	if err != nil {
		if restoreErr := r.restart(current.Current, revision); restoreErr != nil {
			return jobResult(request, "failed_closed", "installation_failed", current.Current), nil
		}
		return reject("preflight_failed")
	}
	installer := &updater.Installer{History: r.history, Directory: images}
	driver.EnableAgentUpdates()
	if preflight.Contract.AgentRecoveryProtocol == 1 {
		originalProbe, probeErr := updater.ProbeAgentImage(r.ctx, images, current.Current, r.required)
		if probeErr == nil && originalProbe.Contract.AgentRecoveryProtocol == 1 {
			driver.EnableAgentRecovery()
		}
	}
	installed, applyErr := installer.Apply(r.ctx, driver, request.ID, revision, candidate, policy)
	r.process = driver.Process()
	if r.process != nil && r.process.Stopping() && !r.process.Stopped() {
		return release.UpdateResult{}, errors.Join(agentprocess.ErrStopUnconfirmed, applyErr)
	}
	switch installed.Phase {
	case "committed":
		if applyErr != nil {
			return release.UpdateResult{}, applyErr
		}
		return jobResult(request, "success", "success", installed.Current), nil
	case "rolled_back":
		return jobResult(request, "rolled_back", "rolled_back", installed.Current), nil
	case "failed_closed":
		if r.process == nil {
			if err := r.restartProtected(installed.Current, revision); err != nil {
				return release.UpdateResult{}, err
			}
		}
		return jobResult(request, "failed_closed", "installation_failed", installed.Current), nil
	default:
		if err := r.restart(current.Current, revision); err != nil {
			return jobResult(request, "failed_closed", "installation_failed", current.Current), nil
		}
		return reject("preflight_failed")
	}
}

func (r *jobRunner) refreshRequired() error {
	key, err := readOriginal(filepath.Join(r.directory, "configuration.key"), 32)
	defer clear(key)
	if err != nil || len(key) != 32 || len(r.key) != 32 || subtle.ConstantTimeCompare(key, r.key) != 1 {
		return errors.New("原配置密钥已缺失或变化，更新父进程不能继续使用缓存身份和日志")
	}
	reader := r.reader
	if reader.Validate() != nil {
		reader = agentstorage.Current(r.required.AgentVersion)
	}
	required, present, err := storage.Check(r.directory, r.nodeID, reader)
	if err != nil || !present {
		return errors.Join(storage.ErrStorage, err)
	}
	r.required = required
	return nil
}

// Called only with stopped Worker ownership. Normal renewal may change the
// certificate fingerprint; it cannot change the node bound by this parent.
func (r *jobRunner) checkStoppedIdentity() error {
	if err := r.refreshRequired(); err != nil {
		return err
	}
	if _, err := identity.LoadKeyPairForNode(filepath.Join(r.directory, "identity"), r.nodeID); err != nil {
		return errors.New("当前节点身份与更新父进程不一致或无法认证；保留原材料和未完成任务")
	}
	return nil
}

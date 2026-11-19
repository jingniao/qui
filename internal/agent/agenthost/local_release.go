package agenthost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/agent/configuration"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/config"
	"proxycontrol/pkg/contracts/release"
)

func localPolicy(c config.Agent) (release.Policy, error) {
	pins := release.OfficialKeyPins
	if c.Updates.Enabled() {
		pins = c.Updates.Keys
	}
	keys, err := release.PinnedKeys(pins)
	if err != nil {
		return release.Policy{}, err
	}
	return release.Policy{Component: "proxy-agent", OS: runtime.GOOS, Architecture: runtime.GOARCH, Now: time.Now().UTC(), Keys: keys}, nil
}

func localCandidates(directory string) string {
	return filepath.Join(directory, "releases", "agent-local-candidates")
}

// LocalRelease verifies the signed raw executable before the supervisor ever
// probes it. The durable, content-addressed candidate also serves interrupted
// local jobs; neither this path nor its restart path consults a network catalogue.
func LocalRelease(ctx context.Context, configPath, manifestPath, binaryPath, version string, install bool) (LocalAgentStatus, error) {
	c, signed, err := prepareLocalInput(ctx, configPath, manifestPath, binaryPath, version)
	if err != nil {
		return LocalAgentStatus{}, err
	}
	operation := "check"
	if install {
		operation = "install"
	}
	return callLocal(ctx, c.DataDirectory, localRequest{Operation: operation, Release: signed})
}

func prepareLocalInput(ctx context.Context, configPath, manifestPath, binaryPath, version string) (config.Agent, release.Signed, error) {
	store, c, err := openJobs(configPath)
	if err != nil {
		return c, release.Signed{}, err
	}
	// Do not create supervised paths on a standalone node. Its authenticated
	// marker and original jobs must already exist before any candidate is staged.
	if err := store.Close(); err != nil {
		return c, release.Signed{}, err
	}
	data, err := readOriginal(manifestPath, release.MaxManifestBytes)
	if err != nil {
		return c, release.Signed{}, err
	}
	signed, err := release.Decode(data)
	if err != nil {
		return c, signed, err
	}
	if signed.Manifest.Version != version {
		return c, signed, fmt.Errorf("Agent 签名清单版本 %s 与目标 %s 不一致", signed.Manifest.Version, version)
	}
	policy, err := localPolicy(c)
	if err != nil {
		return c, signed, err
	}
	artifact, err := os.Open(binaryPath)
	if err != nil {
		return c, signed, err
	}
	_, stageErr := updater.Stage(ctx, localCandidates(c.DataDirectory), signed, policy, artifact)
	if err := errors.Join(stageErr, artifact.Close()); err != nil {
		return c, signed, err
	}
	return c, signed, nil
}

func stageLocalCandidate(ctx context.Context, directory string, signed release.Signed, policy release.Policy) (updater.Candidate, error) {
	path := filepath.Join(localCandidates(directory), signed.Manifest.SHA256+".bin")
	file, err := os.Open(path)
	if err != nil {
		return updater.Candidate{}, fmt.Errorf("本地升级候选缺失，保留任务且不联网下载: %w", err)
	}
	candidate, stageErr := updater.Stage(ctx, localCandidates(directory), signed, policy, file)
	return candidate, errors.Join(stageErr, file.Close())
}

func (r *jobRunner) localStatus() (LocalAgentStatus, error) {
	state, found, err := r.history.Installation("proxy-agent")
	if err != nil || !found {
		return LocalAgentStatus{}, errors.Join(ErrState, err)
	}
	revision, err := reconcile.InspectCurrent(r.directory, r.nodeID, r.key)
	if err != nil {
		return LocalAgentStatus{}, err
	}
	status := LocalAgentStatus{NodeID: r.nodeID, Revision: revision, Current: state.Current}
	status.Running = r.process != nil && !r.process.Stopping() && !r.process.Stopped()
	if r.bootstrapStore != nil {
		return status, nil
	}
	jobs, err := agentjobs.InspectUpgradeStatus(jobsOptions(r.directory, r.nodeID, r.key))
	if err != nil {
		return status, err
	}
	_, baselinePending, err := r.history.PendingBaselineRecovery("proxy-agent")
	if err != nil {
		return status, err
	}
	status.Ready = status.Running && !r.process.Degraded() && revision > 0 && state.Phase != "switching" && state.Phase != "rolling_back" && state.Phase != "failed_closed" && !baselinePending && jobs.Pending == nil && jobs.Recovery == nil
	status.Recovery = status.Running && !baselinePending && jobs.Pending == nil && jobs.Recovery != nil && strings.HasPrefix(jobs.Recovery.Request.ID, localJobPrefix) && jobs.Recovery.Request.Revision == revision
	return status, nil
}

func (r *jobRunner) prepareLocal(signed release.Signed) (LocalAgentStatus, error) {
	status, err := r.localStatus()
	if err != nil {
		return status, err
	}
	if !status.Ready && !status.Recovery {
		if status.Running && status.Revision == 0 {
			return status, errors.New("请先在面板为该节点发布基础配置，再更新 Agent")
		}
		return status, errors.New("Agent 不具备升级条件：服务异常、已有升级任务或恢复尚未完成")
	}
	if status.Recovery {
		if _, err := r.localRecoveryRequest(signed); err != nil {
			return status, err
		}
	}
	c, err := configuration.LoadAgent(r.configPath)
	if err != nil {
		return status, err
	}
	policy, err := localPolicy(c)
	if err != nil {
		return status, err
	}
	if err := r.history.Check(signed, policy); err != nil {
		return status, err
	}
	older, err := localVersionOlder(signed.Manifest.Version, status.Current.Version)
	if err != nil {
		return status, err
	}
	if older {
		return status, fmt.Errorf("不支持把 Agent 降级：目标 %s 低于实际 Agent %s", signed.Manifest.Version, status.Current.Version)
	}
	if err := r.refreshRequired(); err != nil {
		return status, err
	}
	candidate, err := stageLocalCandidate(r.ctx, r.directory, signed, policy)
	if err != nil {
		return status, err
	}
	images := filepath.Join(r.directory, "releases", "agent-installed")
	preflight, err := updater.PreflightAgent(r.ctx, images, candidate, policy, r.required)
	if err != nil {
		return status, err
	}
	if preflight.Contract.ManagedProtocol != 1 || preflight.Contract.AgentUpdateProtocol != 1 {
		return status, updater.ErrAgentPreflight
	}
	path, err := updater.ResolveImage(r.ctx, images, "proxy-agent", preflight.Image)
	if err != nil {
		return status, err
	}
	ctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, "--local-agent-contract").CombinedOutput()
	if err != nil {
		return status, fmt.Errorf("目标 Agent 不支持本地升级入口: %w: %s", err, output)
	}
	if string(output) != "proxy-agent-local-v1 "+signed.Manifest.Version+"\n" {
		return status, errors.New("目标 Agent 的本地升级契约不匹配")
	}
	return status, nil
}

// Called only by the supervisor event loop, so a local install and a polled web
// install never run concurrently. Store.Queue additionally reserves against a
// web request arriving while this preflight is in progress.
func (r *jobRunner) handleLocal(request localRequest, lease <-chan struct{}) (status LocalAgentStatus, resultErr error) {
	if request.Operation == "status" {
		return r.localStatus()
	}
	if request.Operation != "check" && request.Operation != "install" && request.Operation != "reserve" {
		return status, errors.New("unsupported local Agent operation")
	}
	status, err := r.prepareLocal(request.Release)
	if err != nil || request.Operation == "check" {
		return status, err
	}
	if request.Operation == "reserve" {
		if lease == nil {
			return status, errors.New("bootstrap reservation requires a live local lease")
		}
		store, err := agentjobs.Open(jobsOptions(r.directory, r.nodeID, r.key))
		if err != nil {
			return status, err
		}
		if store.Pending() != nil || store.NeedsRecovery() && !status.Recovery {
			return status, errors.Join(agentjobs.ErrBusy, store.Close())
		}
		select {
		case <-lease:
			return status, errors.Join(errors.New("bootstrap reservation caller disconnected"), store.Close())
		default:
		}
		r.bootstrapStore, r.bootstrapDone = store, lease
		return status, nil
	}
	if status.Recovery {
		if err := r.recoverLocal(request.Release); err != nil {
			return status, err
		}
		status, err = r.prepareLocal(request.Release)
		if err != nil {
			return status, err
		}
	}
	manifest := request.Release.Manifest
	if status.Current.Version == manifest.Version && status.Current.SHA256 == manifest.SHA256 {
		return status, nil
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return status, err
	}
	queued := release.AgentUpdateRequest{SchemaVersion: 1, ID: localJobPrefix + hex.EncodeToString(nonce), NodeID: r.nodeID, Revision: status.Revision, Operation: "install", Release: request.Release}
	result, err := r.runLocalJob(queued)
	if err != nil {
		return status, err
	}
	status, err = r.localStatus()
	if err != nil {
		return status, err
	}
	if result.Status != "success" {
		return status, errors.Join(fmt.Errorf("本地 Agent 升级失败：%s (%s)，当前版本 %s", result.Status, result.Code, status.Current.Version), r.localError)
	}
	if !status.Ready || status.Current.Version != manifest.Version || status.Current.SHA256 != manifest.SHA256 {
		return status, errors.New("本地 Agent 升级结果与实际工作进程不一致")
	}
	return status, nil
}

func (r *jobRunner) runLocalJob(request release.AgentUpdateRequest) (*release.UpdateResult, error) {
	store, err := agentjobs.Open(jobsOptions(r.directory, r.nodeID, r.key))
	if err != nil {
		return nil, err
	}
	if err := errors.Join(store.Queue(request), store.Close()); err != nil {
		return nil, err
	}
	r.localError = nil
	if err := r.poll(); err != nil {
		return nil, err
	}
	store, err = agentjobs.Open(jobsOptions(r.directory, r.nodeID, r.key))
	if err != nil {
		return nil, err
	}
	job, found := store.Get(request.ID)
	if err := store.Close(); err != nil {
		return nil, err
	}
	if !found || job.Result == nil {
		return nil, errors.New("本地 Agent 升级结果未落盘，请检查保留的任务记录")
	}
	return job.Result, nil
}

// Recovery binds the original local request, including its immutable signed
// release and revision. It never erases a failed attempt or invents an install.
func (r *jobRunner) recoverLocal(target release.Signed) error {
	original, err := r.localRecoveryRequest(target)
	if err != nil {
		return err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	request := original
	request.ID = localJobPrefix + hex.EncodeToString(nonce)
	request.Operation, request.OriginalID = "recover", original.ID
	result, err := r.runLocalJob(request)
	if err != nil {
		return err
	}
	if result.Status != "success" && result.Status != "rolled_back" {
		return errors.Join(fmt.Errorf("本地 Agent 恢复失败：%s (%s)", result.Status, result.Code), r.localError)
	}
	return nil
}

func (r *jobRunner) localRecoveryRequest(target release.Signed) (release.AgentUpdateRequest, error) {
	state, err := agentjobs.InspectUpgradeStatus(jobsOptions(r.directory, r.nodeID, r.key))
	if err != nil {
		return release.AgentUpdateRequest{}, err
	}
	if state.Pending != nil || state.Recovery == nil || !strings.HasPrefix(state.Recovery.Request.ID, localJobPrefix) {
		return release.AgentUpdateRequest{}, agentjobs.ErrBusy
	}
	original := state.Recovery.Request
	if original.Release.Manifest.SHA256 != target.Manifest.SHA256 || original.Release.Manifest.Version != target.Manifest.Version {
		return original, fmt.Errorf("须先用原目标版本 %s 的清单和归档收尾本地升级，再更新其他版本", original.Release.Manifest.Version)
	}
	return original, nil
}

func (r *jobRunner) releaseBootstrap() error {
	if r.bootstrapStore == nil {
		return nil
	}
	store := r.bootstrapStore
	r.bootstrapStore, r.bootstrapDone = nil, nil
	return store.Close()
}

var localVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.([1-9][0-9]*))?$`)

// Match the shell's release grammar and compare decimal strings without integer
// overflow. Unknown versions are an explicit error at this operator boundary.
func localVersionOlder(target, current string) (bool, error) {
	a, b := localVersionPattern.FindStringSubmatch(target), localVersionPattern.FindStringSubmatch(current)
	if a == nil || b == nil {
		return false, fmt.Errorf("无法比较 Agent 发布版本 %q 与 %q", target, current)
	}
	less := func(x, y string) bool { return len(x) < len(y) || len(x) == len(y) && x < y }
	for i := 1; i <= 3; i++ {
		if a[i] != b[i] {
			return less(a[i], b[i]), nil
		}
	}
	if a[5] == "" {
		return false, nil
	}
	return b[5] == "" || less(a[5], b[5]), nil
}

func (r *jobRunner) recordLocalFailure(id string, err error) {
	if strings.HasPrefix(id, localJobPrefix) && err != nil {
		r.localError = err
		slog.Error("local Agent update failed", "id", id, "error", err)
	}
}

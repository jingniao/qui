package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/config"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

// coreUpdates executes one direct-URL core update at a time. The command
// carries an official release asset URL, the expected stable version and the
// archive SHA-256; the Agent verifies the archive, extracts the single
// expected executable, checks its own version and switches under the existing
// supervised rollback.
type coreUpdates struct {
	cfg       config.Agent
	nodeID    string
	component string
	manager   *reconcile.Reconciler
	source    *updater.CoreSource
	mu        sync.Mutex
	active    *release.CoreUpdateRequest
	requests  chan release.CoreUpdateRequest
	results   chan *pb.CoreUpdateResult
	cancel    context.CancelFunc
	done      chan struct{}
}

func newCoreUpdates(ctx context.Context, c config.Agent, nodeID string, manager *reconcile.Reconciler) (*coreUpdates, error) {
	if manager == nil {
		return nil, nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	w := &coreUpdates{cfg: c, nodeID: nodeID, component: coreComponent(c), manager: manager, source: updater.NewCoreSource(), requests: make(chan release.CoreUpdateRequest, 1), results: make(chan *pb.CoreUpdateResult, 1), cancel: cancel, done: make(chan struct{})}
	go w.run(workCtx)
	return w, nil
}

func (w *coreUpdates) run(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-w.requests:
			result, err := w.execute(ctx, request)
			if err == nil {
				data, _ := json.Marshal(result)
				select {
				case w.results <- &pb.CoreUpdateResult{ResultJson: data}:
				case <-ctx.Done():
				}
			}
			w.mu.Lock()
			w.active = nil
			w.mu.Unlock()
		}
	}
}

func (w *coreUpdates) Close() {
	if w != nil {
		w.cancel()
		<-w.done
	}
}

func (w *coreUpdates) submit(command *pb.CoreUpdateCommand) error {
	request, err := release.DecodeCoreUpdateRequest(command.RequestJson)
	if err != nil || request.NodeID != w.nodeID {
		return release.ErrUpdate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active != nil {
		if *w.active == request {
			return nil
		}
		return errors.New("different core update received while executing")
	}
	w.active = &request
	select {
	case w.requests <- request:
		return nil
	default:
		w.active = nil
		return errors.New("core update queue is full")
	}
}

func (w *coreUpdates) finish(request release.CoreUpdateRequest, status, code string, state updater.Installation) (release.UpdateResult, error) {
	result := coreUpdateResult(request, status, code, state)
	if result.ValidateCore() != nil {
		return release.UpdateResult{}, release.ErrUpdate
	}
	return result, nil
}

// execute performs the whole update and always returns a validated result: the
// node never reports an outcome it cannot justify from its own state.
func (w *coreUpdates) execute(parent context.Context, request release.CoreUpdateRequest) (release.UpdateResult, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	if request.Validate() != nil || request.NodeID != w.nodeID {
		return release.UpdateResult{}, release.ErrUpdate
	}
	if request.Component != w.component || request.Revision != w.manager.Current() {
		return w.finish(request, "rejected", "scope_mismatch", updater.Installation{})
	}
	if w.manager.CoreUpdateHeld() {
		return w.finish(request, "failed_closed", "installation_failed", updater.Installation{})
	}
	history, err := openCoreHistory(w.cfg, w.nodeID)
	if err != nil {
		return w.finish(request, "failed_closed", "history_invalid", updater.Installation{})
	}
	defer history.Close()
	state, exists, err := history.Installation(w.component)
	if err != nil {
		return w.finish(request, "failed_closed", "history_invalid", updater.Installation{})
	}
	if exists && coreInstallationPending(state) {
		return w.finish(request, "failed_closed", "installation_failed", state)
	}
	extracted, err := w.source.FetchCore(ctx, filepath.Join(w.cfg.DataDirectory, "releases", "core-staging"), w.component, request.URL, request.SHA256)
	if err != nil {
		return w.finish(request, "rejected", coreDownloadCode(err), updater.Installation{})
	}
	defer os.RemoveAll(filepath.Dir(extracted))
	image, err := updater.InspectImage(ctx, w.component, extracted, request.Version)
	if err != nil {
		return w.finish(request, "rejected", "archive_invalid", updater.Installation{})
	}
	image.Sequence = request.Sequence
	installer := &updater.Installer{History: history, Directory: filepath.Join(w.cfg.DataDirectory, "releases", "installed")}
	var applied updater.Installation
	rejected := ""
	err = w.manager.WithCoreUpdate(ctx, request.Revision, func(lease *reconcile.CoreUpdateLease) error {
		detected, detectErr := lease.DetectVersion(ctx, extracted)
		if detectErr != nil {
			rejected = "preflight_failed"
			return detectErr
		}
		if detected != request.Version {
			rejected = "version_mismatch"
			return errors.New("downloaded core reports a different version")
		}
		var applyErr error
		applied, applyErr = installer.ApplyImage(ctx, lease, request.ID, request.Revision, image, extracted)
		return applyErr
	})
	if rejected != "" {
		return w.finish(request, "rejected", rejected, updater.Installation{})
	}
	if err == nil && applied.Phase == "committed" {
		return w.finish(request, "success", "success", applied)
	}
	if applied.Phase == "rolled_back" {
		return w.finish(request, "rolled_back", "rolled_back", applied)
	}
	return w.finish(request, "failed_closed", "installation_failed", applied)
}

func coreUpdateResult(request release.CoreUpdateRequest, status, code string, state updater.Installation) release.UpdateResult {
	result := release.UpdateResult{ID: request.ID, NodeID: request.NodeID, Revision: request.Revision, TargetDigest: request.SHA256, Status: status, Code: code}
	if state.Current.SHA256 != "" {
		result.InstalledDigest = state.Current.SHA256
		result.InstalledVersion = state.Current.Version
		result.InstalledSequence = state.Current.Sequence
	}
	return result
}

func coreDownloadCode(err error) string {
	switch {
	case errors.Is(err, updater.ErrCoreChecksum):
		return "checksum_mismatch"
	case errors.Is(err, updater.ErrCoreArchive):
		return "archive_invalid"
	case errors.Is(err, updater.ErrCoreSource):
		return "unsupported_url"
	default:
		return "download_failed"
	}
}

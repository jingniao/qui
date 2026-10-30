package control

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"proxycontrol/internal/agent/configuration"
	"proxycontrol/internal/agent/core"
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
	makeRuntime func(string, string, string, string) core.Runtime
	children    map[string]*coreUpdates
	cfg         config.Agent
	nodeID      string
	component   string
	manager     *reconcile.Reconciler
	source      *updater.CoreSource
	// capabilities is the node's shared capability holder and detect re-reads
	// the installed core, so a committed or rolled-back image immediately
	// replaces what every later session reports.
	capabilities *capabilityState
	detect       func(context.Context) (*pb.Capabilities, error)
	mu           sync.Mutex
	active       *release.CoreUpdateRequest
	requests     chan release.CoreUpdateRequest
	results      chan *pb.CoreUpdateResult
	// pending holds the results the panel has not acknowledged yet, oldest
	// first, and rejections counts how often the panel refused the head with
	// the permanent rejection. Both live as long as the worker, so a result
	// whose acknowledgment a dropped stream swallowed is sent again by the next
	// session instead of disappearing with the one that lost it. They are in
	// memory only: an Agent restart forgets them, which the panel covers with
	// CloseStaleCoreUpdates on the next connection.
	pending    []*pb.CoreUpdateResult
	rejections int
	cancel     context.CancelFunc
	done       chan struct{}
}

// coreUpdateRejectLimit is how many consecutive permanent rejections of one
// result the Agent tolerates before it drops it. The panel answers a receipt
// with FailedPrecondition and the rejection message when it will never store
// it, for example because the Owner cleared the task, so keeping the result
// would fail every later session and keep the node reconnecting for nothing.
const coreUpdateRejectLimit = 3

func newCoreUpdates(ctx context.Context, c config.Agent, nodeID string, manager *reconcile.Reconciler, capabilities *capabilityState, detect func(context.Context) (*pb.Capabilities, error)) (*coreUpdates, error) {
	if manager == nil {
		return nil, nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	w := &coreUpdates{cfg: c, nodeID: nodeID, component: coreComponent(c), manager: manager, source: updater.NewCoreSource(), capabilities: capabilities, detect: detect, requests: make(chan release.CoreUpdateRequest, 1), results: make(chan *pb.CoreUpdateResult, 1), cancel: cancel, done: make(chan struct{})}
	w.makeRuntime = coreRuntime
	go w.run(workCtx)
	if multi, ok := manager.Adapter.(*core.MultiRuntime); ok {
		w.children = map[string]*coreUpdates{}
		for _, name := range []string{"xray", "sing-box"} {
			entry := configuration.Core{Name: name}
			if runtime, ok := multi.Component(name).(core.BinaryRuntime); ok {
				entry.Path = runtime.ExecutablePath()
			}
			if entry.Name == w.component {
				continue
			}
			childCfg := c
			childCfg.CoreName, childCfg.CorePath = entry.Name, entry.Path
			childCtx, childCancel := context.WithCancel(ctx)
			child := &coreUpdates{cfg: childCfg, nodeID: nodeID, component: entry.Name, manager: manager, source: updater.NewCoreSource(), capabilities: capabilities, detect: detect, requests: make(chan release.CoreUpdateRequest, 1), results: make(chan *pb.CoreUpdateResult, 1), cancel: childCancel, done: make(chan struct{})}
			child.makeRuntime = coreRuntime
			w.children[entry.Name] = child
			go child.run(childCtx)
		}
	}
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
				if coreUpdateChangedTheCore(result.Status) {
					w.refreshCapabilities(ctx)
				}
				data, _ := json.Marshal(result)
				select {
				case w.results <- &pb.CoreUpdateResult{ResultJson: data, Component: request.Component}:
				case <-ctx.Done():
				}
			}
			w.mu.Lock()
			w.active = nil
			w.mu.Unlock()
		}
	}
}

// A committed image is the new running core and a rollback restored the retained
// one; every other outcome leaves the running binary untouched.
func coreUpdateChangedTheCore(status string) bool {
	return status == "success" || status == "rolled_back"
}

// collect moves every finished result into the pending list, keeping the order
// in which the updates completed. The list outlives the session that sends it,
// so a result stays until the panel acknowledges it.
func (w *coreUpdates) collect() {
	for _, child := range w.children {
		child.collect()
		for {
			r := child.next()
			if r == nil {
				break
			}
			w.push(r)
			child.acknowledged()
		}
	}
	for {
		select {
		case result := <-w.results:
			w.push(result)
		default:
			return
		}
	}
}

func (w *coreUpdates) push(result *pb.CoreUpdateResult) {
	w.mu.Lock()
	w.pending = append(w.pending, result)
	w.mu.Unlock()
}

// next reports the oldest result the panel has not acknowledged yet, or nil.
func (w *coreUpdates) next() *pb.CoreUpdateResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return nil
	}
	return w.pending[0]
}

// acknowledged drops the head because the panel stored it and answered it. Only
// a nil send result gets here: every failure keeps the result for the next
// session.
func (w *coreUpdates) acknowledged() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) > 0 {
		w.pending = w.pending[1:]
	}
	w.rejections = 0
}

// rejected records one permanent rejection of the head. The panel ends the
// stream with FailedPrecondition and the rejection message when it will never
// store that receipt, for example because the Owner cleared the task; the
// result has to stay until it repeats, because a single reconnect must not lose
// a result the panel simply could not accept yet. After coreUpdateRejectLimit
// consecutive rejections of the same result it is dropped, so the node stops
// reconnecting only to fail the same way.
func (w *coreUpdates) rejected(logger *slog.Logger) {
	w.mu.Lock()
	w.rejections++
	var dropped *pb.CoreUpdateResult
	if w.rejections >= coreUpdateRejectLimit && len(w.pending) > 0 {
		dropped = w.pending[0]
		w.pending = w.pending[1:]
		w.rejections = 0
	}
	w.mu.Unlock()
	if dropped == nil {
		return
	}
	// The task ID is the only field of the result a log line needs, and it is
	// the one an operator can look up in the panel.
	logger.Warn("core update result dropped after repeated rejection", "attempts", coreUpdateRejectLimit, "task_id", coreUpdateTaskID(dropped))
}

// coreUpdateResultRejected reports the panel's permanent refusal of a core
// update receipt: it ends the stream with FailedPrecondition and this exact
// message when it will never store the result. Everything else, including a
// transport failure or a panel that is merely unavailable, stays retryable.
func coreUpdateResultRejected(err error) bool {
	return status.Code(err) == codes.FailedPrecondition && status.Convert(err).Message() == "core update result rejected"
}

// coreUpdateTaskID names the task a result belongs to, for log lines only.
func coreUpdateTaskID(result *pb.CoreUpdateResult) string {
	decoded, err := release.DecodeCoreUpdateResult(result.ResultJson)
	if err != nil {
		return ""
	}
	return decoded.ID
}

// refreshCapabilities replaces the node's reported capabilities with the ones of
// the core that is installed now. A failed detection keeps the previous value,
// which stays consistent with the core that is still running.
func (w *coreUpdates) refreshCapabilities(ctx context.Context) {
	if w.detect == nil || w.capabilities == nil {
		return
	}
	detected, err := w.detect(ctx)
	if err != nil || detected == nil {
		return
	}
	w.capabilities.Store(detected)
}

func (w *coreUpdates) Close() {
	if w != nil {
		for _, child := range w.children {
			child.Close()
		}
		w.cancel()
		<-w.done
	}
}

func (w *coreUpdates) submit(command *pb.CoreUpdateCommand) error {
	request, err := release.DecodeCoreUpdateRequest(command.RequestJson)
	if err != nil || request.NodeID != w.nodeID {
		return release.ErrUpdate
	}
	if command.Component != "" && command.Component != request.Component {
		return release.ErrUpdate
	}
	if child := w.children[request.Component]; child != nil {
		return child.submit(command)
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
	if inventory, err := configuration.ReadInventory(w.cfg.DataDirectory, w.nodeID); err != nil {
		return w.finish(request, "failed_closed", "history_invalid", updater.Installation{})
	} else if entry := inventory.Entries[request.Component]; entry.RequestID == request.ID {
		if entry.Disabled || entry.Revision != request.Revision || entry.Sequence != request.Sequence || entry.Digest != request.SHA256 || entry.Version != request.Version || entry.URL != request.URL {
			return w.finish(request, "rejected", "scope_mismatch", updater.Installation{})
		}
		return w.finish(request, "success", "success", updater.Installation{Current: updater.Image{SHA256: entry.InstalledDigest, Version: entry.Version, Sequence: entry.Sequence}})
	}
	if multi, ok := w.manager.Adapter.(*core.MultiRuntime); ok && multi.Component(request.Component) == nil {
		return w.addCore(ctx, request)
	}
	if request.Component != w.component {
		cfg := w.cfg
		cfg.CoreName = request.Component
		if multi, ok := w.manager.Adapter.(*core.MultiRuntime); ok {
			if b, ok := multi.Component(request.Component).(core.BinaryRuntime); ok {
				cfg.CorePath = b.ExecutablePath()
			}
		}
		worker := &coreUpdates{cfg: cfg, nodeID: w.nodeID, component: request.Component, manager: w.manager, source: w.source, makeRuntime: w.makeRuntime}
		return worker.execute(ctx, request)
	}
	if request.Component != w.component || request.Revision != w.manager.Current() {
		return w.finish(request, "rejected", "scope_mismatch", updater.Installation{})
	}
	if w.manager.CoreComponentHeld(w.component) {
		return w.finish(request, "failed_closed", "installation_failed", updater.Installation{})
	}
	history, err := openOrCreateCoreHistory(w.cfg, w.nodeID)
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
	staging := filepath.Join(w.cfg.DataDirectory, "releases", "core-staging")
	if _, ok := w.manager.Adapter.(*core.MultiRuntime); ok {
		staging = filepath.Join(staging, w.component)
		defer os.Remove(staging)
	}
	extracted, err := w.source.FetchCore(ctx, staging, w.component, request.URL, request.SHA256)
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
	err = w.manager.WithCoreComponentUpdate(ctx, request.Revision, w.component, func(lease *reconcile.CoreUpdateLease) error {
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

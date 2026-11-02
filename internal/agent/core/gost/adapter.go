package gost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/scheduler"
	"proxycontrol/pkg/contracts/traffic"
)

// Adapter runs the pinned GOST release as one component of the node. GOST is a
// deliberate restart-on-apply core: it can reload in place, but a reload is not
// transactional and gives no clean failure signal, so a revision stops the exact
// child, installs the new file and starts again.
type Adapter struct {
	Path      string
	Directory string
	NodeID    string

	mu          sync.Mutex
	hooks       core.RuntimeHooks
	process     *exec.Cmd
	done        chan error
	stopPending bool
	startedAt   time.Time
	log         childLog
	marker      core.RunMarker
	current     core.RenderedConfig
	history     map[int64]core.RenderedConfig
	sources     map[string]bool
	metrics     metricsStatus
}

// metricsStatus is the traffic-source state of the running epoch. A failed scrape
// is a traffic-source failure only: it never makes an otherwise serving core
// unhealthy, and it never invents a zero usage.
type metricsStatus struct {
	sampled time.Time
	failed  bool
}

func (a *Adapter) Name() string { return CoreName }

func (a *Adapter) ConfigureRuntime(hooks core.RuntimeHooks) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hooks = hooks
}

func (a *Adapter) ExecutablePath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Path
}

// SupportsAccountTelemetry is false: GOST has no per-user counter, so account
// bytes and connection snapshots stay unknown instead of becoming zero.
func (a *Adapter) SupportsAccountTelemetry() bool { return false }

// OutboundHealth reports only probes that were actually configured; G1 renders
// none, so the map is empty rather than optimistic.
func (a *Adapter) OutboundHealth() map[string]scheduler.Health {
	return map[string]scheduler.Health{}
}

// SelectExecutable switches to a downloaded binary after its process stopped.
func (a *Adapter) SelectExecutable(ctx context.Context, path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.process != nil {
		return errors.New("gost executable cannot change while running")
	}
	if err := a.lifecycleReadyLocked(); err != nil {
		return err
	}
	if _, err := Detect(ctx, path); err != nil {
		return err
	}
	a.Path = path
	a.current = core.RenderedConfig{}
	a.history = nil
	return nil
}

// ValidateExecutable is the preflight of a downloaded core: it identifies the
// binary and syntax-checks the configuration with it before the image is
// switched in.
func (a *Adapter) ValidateExecutable(ctx context.Context, path string, configuration core.RenderedConfig) (string, error) {
	version, err := a.DetectVersion(ctx, path)
	if err != nil {
		return "", err
	}
	validator := &Adapter{Path: path, Directory: a.Directory}
	if err := validator.Validate(ctx, configuration); err != nil {
		return "", err
	}
	return version, nil
}

// childLog keeps the tail of the child's stderr in a fixed buffer, so a failed
// start can report GOST's own fatal line while the Agent never holds a pipe that
// grows or blocks. Only the warning level is rendered, and G1 configurations
// carry no credential, so the retained text names addresses and services.
type childLog struct {
	mu   sync.Mutex
	data []byte
}

const childLogLimit = 4096

func (l *childLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.data = append(l.data, data...)
	if len(l.data) > childLogLimit {
		l.data = append([]byte(nil), l.data[len(l.data)-childLogLimit:]...)
	}
	return len(data), nil
}

func (l *childLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.data = nil
}

func (l *childLog) tail() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.TrimSpace(string(l.data))
}

var _ io.Writer = (*childLog)(nil)

// Apply validates the revision, stops the exact child and starts the new one. A
// candidate that does not settle restores the previous revision; every error path
// after a successful stop therefore leaves the node with a core or reports that it
// could not.
func (a *Adapter) Apply(ctx context.Context, configuration core.RenderedConfig) (core.ApplyResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := core.ApplyResult{Revision: configuration.Revision}
	if err := a.lifecycleReadyLocked(); err != nil {
		result.Code = "core_lifecycle_unconfirmed"
		return result, err
	}
	if err := a.Validate(ctx, configuration); err != nil {
		result.Code = "validation_failed"
		result.Message = err.Error()
		return result, err
	}
	if err := a.stopLocked(); err != nil {
		result.Code = "stop_failed"
		result.Message = "could not stop previous core"
		return result, err
	}
	if err := a.startLocked(ctx, configuration); err != nil {
		result.Code = "start_failed"
		result.Message = fmt.Sprintf("gost startup or settle check failed: %v", err)
		if a.current.Revision > 0 {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			rollbackErr := a.startLocked(rollbackCtx, a.current)
			cancel()
			if rollbackErr != nil {
				result.Code = "rollback_failed"
				result.Message = fmt.Sprintf("gost failed and the previous revision could not restart: %v", rollbackErr)
				return result, errors.Join(err, rollbackErr)
			}
			result.RolledBack = true
		}
		return result, errors.New(result.Message)
	}
	if a.history == nil {
		a.history = map[int64]core.RenderedConfig{}
	}
	a.history[configuration.Revision] = configuration
	result.Success = true
	result.Code = "success"
	result.Message = "configuration applied and healthy"
	return result, nil
}

// Rollback starts a retained revision again. The caller either kept it in memory
// here or restores its encrypted snapshot through the Reconciler.
func (a *Adapter) Rollback(ctx context.Context, revision int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	target, exists := a.history[revision]
	if !exists {
		return errors.New("rollback revision is not loaded; use Reconciler to restore its encrypted snapshot")
	}
	if err := a.stopLocked(); err != nil {
		return err
	}
	return a.startLocked(ctx, target)
}

func (a *Adapter) startLocked(ctx context.Context, configuration core.RenderedConfig) (resultErr error) {
	if a.stopPending {
		return core.ErrCoreStopUnconfirmed
	}
	if err := a.lifecycleReadyLocked(); err != nil {
		return err
	}
	var config Config
	if err := json.Unmarshal(configuration.Data, &config); err != nil {
		return errors.New("invalid gost native configuration")
	}
	executable, err := exec.LookPath(a.Path)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return err
	}
	data, err := EncodeYAML(config)
	if err != nil {
		return err
	}
	epoch, err := id.New()
	if err != nil {
		return err
	}
	if err := writeConfig(a.Directory, data); err != nil {
		return err
	}
	// A killed core leaves its socket file behind and cannot bind over it.
	socket := filepath.Join(a.Directory, MetricsSocket)
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Record the pending child before it exists, so a crash in the start window
	// cannot leave an orphan core without a durable marker.
	if err := a.runMarker().Claim(epoch, configuration.Revision); err != nil {
		return err
	}
	a.log.reset()
	command := exec.Command(absolute, "-C", filepath.Join(a.Directory, ConfigFile))
	command.Env = core.CoreEnvironment()
	// The core must also die with the Agent when the supervisor only signals the
	// Agent PID (OpenRC supervise-daemon).
	command.SysProcAttr = core.CoreProcessAttributes()
	command.Stdout = io.Discard
	// Only warn lines are rendered, so the bounded tail is the diagnostic of a
	// refused start and nothing else.
	command.Stderr = &a.log
	if err := command.Start(); err != nil {
		return errors.Join(err, a.runMarker().Abandon())
	}
	a.startedAt = time.Now()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	accepted := false
	defer func() {
		if accepted {
			return
		}
		// Kill and prove the exit, or keep the exact child so no replacement can
		// start for it.
		_ = command.Process.Kill()
		if reapErr := core.ReapProcess(done, core.StopReapTimeout); reapErr != nil {
			a.process, a.done, a.stopPending = command, done, true
			resultErr = errors.Join(resultErr, reapErr)
			return
		}
		resultErr = errors.Join(resultErr, a.runMarker().ConfirmExit())
	}()
	if err := a.runMarker().Bind(command.Process.Pid); err != nil {
		return err
	}
	if err := settle(ctx, command.Process.Pid, done, configuration, socket, a.log.tail); err != nil {
		return err
	}
	// The socket lives inside the 0700 runtime directory; its own mode is tightened
	// too, where the platform keeps unix socket permissions. A platform that does
	// not is the one GOST is not installed on, so the failure is not fatal here.
	_ = os.Chmod(socket, 0600)
	a.process, a.done = command, done
	a.current = configuration
	a.sources = forwardServiceNames(config)
	a.metrics = metricsStatus{}
	accepted = true
	return nil
}

// stopLocked samples the counters that are still reachable and kills the exact
// child. A failed final sample is reported through the traffic-source status and
// never blocks the replacement: GOST's counters reset with the process anyway, and
// a metrics-only failure must not keep a revision from being applied.
func (a *Adapter) stopLocked() error {
	if a.process == nil {
		return a.lifecycleReadyLocked()
	}
	select {
	case <-a.done:
		a.process, a.done, a.stopPending = nil, nil, false
		a.sources = nil
		return a.runMarker().ConfirmExit()
	default:
	}
	if a.hooks.TrafficSink != nil && len(a.sources) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.sampleTrafficLocked(ctx)
		cancel()
	}
	return a.forceStopLocked()
}

func (a *Adapter) forceStopLocked() error {
	if a.process == nil {
		return a.lifecycleReadyLocked()
	}
	if err := a.process.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		// Killing failed for a reason other than the child already being gone, so
		// its exit is unconfirmed: keep the original child and refuse a
		// replacement start.
		a.stopPending = true
		return err
	}
	if err := core.ReapProcess(a.done, core.StopReapTimeout); err != nil {
		a.stopPending = true
		return err
	}
	a.process, a.done, a.stopPending = nil, nil, false
	a.sources = nil
	return a.runMarker().ConfirmExit()
}

func (a *Adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	err := a.stopLocked()
	// A failed final sample prevents configuration replacement, but an explicit
	// Agent shutdown must still stop its child rather than orphan the process.
	if err == nil || errors.Is(err, core.ErrCoreStopUnconfirmed) {
		return err
	}
	return errors.Join(err, a.forceStopLocked())
}

// UnconfirmedStop reports that the last forced stop could not confirm the exact
// child's exit, or that a durable marker still names an unproven child.
func (a *Adapter) UnconfirmedStop() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopPending {
		return true
	}
	observation, err := a.runMarker().Observe(a.process != nil)
	return err != nil || observation == core.CoreRunBlocked
}

// CoreStartedAt is the start time of the running child; the zero value means
// nothing was measured and is never rendered as an uptime.
func (a *Adapter) CoreStartedAt() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.process == nil {
		return time.Time{}
	}
	return a.startedAt
}

func (a *Adapter) lifecycleReadyLocked() error { return a.runMarker().Ready(a.process != nil) }

// SampleTraffic reports the cumulative node counters of the running process to the
// existing traffic sink. GOST has no account counter, so this is the node total and
// nothing else; the panel keeps one cursor per (node, epoch) and needs no change.
func (a *Adapter) SampleTraffic(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sampleTrafficLocked(ctx)
}

func (a *Adapter) sampleTrafficLocked(ctx context.Context) error {
	if a.process == nil || a.hooks.TrafficSink == nil {
		return nil
	}
	upload, download, err := readMetrics(ctx, filepath.Join(a.Directory, MetricsSocket), a.sources)
	if err != nil {
		a.metrics.failed = true
		return err
	}
	a.metrics.sampled, a.metrics.failed = time.Now().UTC(), false
	counter, err := core.NodeCounter(CoreName, a.marker.Epoch, a.current.Revision, upload, download)
	if err != nil {
		return err
	}
	return a.hooks.TrafficSink(ctx, []traffic.Counter{counter})
}

// CollectStats is the same node total as a single batch, for callers that read the
// counters directly instead of through the sink.
func (a *Adapter) CollectStats(ctx context.Context) (core.StatsBatch, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.process == nil {
		return core.StatsBatch{}, errors.New("core not running")
	}
	upload, download, err := readMetrics(ctx, filepath.Join(a.Directory, MetricsSocket), a.sources)
	if err != nil {
		a.metrics.failed = true
		return core.StatsBatch{}, err
	}
	a.metrics.sampled, a.metrics.failed = time.Now().UTC(), false
	return core.StatsBatch{Upload: upload, Download: download}, nil
}

// TrafficSourceStatus reports the metrics socket as the traffic source of this
// core. A failed scrape is exactly this status: it never becomes a zero usage and
// never restarts a forwarding core.
func (a *Adapter) TrafficSourceStatus() *pb.TrafficSourceStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	running := a.process != nil
	// An idle core (installed, no forward assigned) or one whose run marker is
	// gone has no process identity to attribute counters to; like Xray without a
	// statistics epoch, that is "unavailable", which the panel accepts without one.
	if a.marker.Epoch == "" || a.current.Revision < 1 {
		return &pb.TrafficSourceStatus{Status: "unavailable", ObservedAt: timestamppb.Now()}
	}
	status := &pb.TrafficSourceStatus{Status: "unobserved", CoreEpoch: a.marker.Epoch, Revision: a.current.Revision, ConfiguredSources: uint64(len(a.sources)), CoreRunning: running, ObservedAt: timestamppb.Now()}
	switch {
	case a.metrics.failed:
		status.Status = "sampling_failed"
	case !running:
		status.Status = "not_running"
	case !a.metrics.sampled.IsZero():
		status.Status = "covered"
		status.ObservedSources = status.ConfiguredSources
		status.LastSuccessAt = timestamppb.New(a.metrics.sampled)
	}
	return status
}

// serviceNames is the set of every rendered service name, which the settle gate
// uses to compare what the child actually bound.
func serviceNames(config Config) map[string]bool {
	names := map[string]bool{}
	for _, service := range config.Services {
		names[service.Name] = true
	}
	return names
}

// writePrivate creates a 0600 file through a random name, syncs it and returns the
// path. A caller that keeps the file renames it; everything else removes it.
func writePrivate(directory, pattern string, data []byte) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	success := false
	defer func() {
		file.Close()
		if !success {
			os.Remove(path)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	success = true
	return path, nil
}

// writeConfig installs the live configuration under its fixed name in one rename,
// so a crash between two applies never leaves a truncated file behind.
func writeConfig(directory string, data []byte) error {
	path, err := writePrivate(directory, "."+ConfigFile+"-*", data)
	if err != nil {
		return err
	}
	if err := os.Rename(path, filepath.Join(directory, ConfigFile)); err != nil {
		os.Remove(path)
		return err
	}
	return vault.SyncDir(directory)
}

var (
	_ core.Runtime       = (*Adapter)(nil)
	_ core.BinaryRuntime = (*Adapter)(nil)
)

// runMarker binds the shared core-run marker to this adapter's runtime
// directory, node and core name. Adapters are plain values built by the node
// wiring and by tests, so the binding is resolved on use, as in the Xray and
// sing-box adapters.
func (a *Adapter) runMarker() *core.RunMarker {
	a.marker.Directory, a.marker.NodeID, a.marker.Core = a.Directory, a.NodeID, CoreName
	return &a.marker
}

package xray

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/gate"
	"proxycontrol/internal/agent/observer"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
	access "proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"proxycontrol/pkg/contracts/traffic"
	"sync"
	"time"
)

type Adapter struct {
	startedAt          time.Time
	connectionObserver observer.Store
	deviceGate         gate.Lease
	gateDigest         string
	gateRecovery       bool
	gateFailed         bool
	gateRunner         func(context.Context, []string, io.Writer) error
	TrafficSink        func(context.Context, []traffic.Counter) error
	EnableStats        bool
	statsPort          int
	statsEpoch         string
	counterSources     core.CounterSources
	NodeID             string
	AccessSink         func(access.Rollup) error
	AccessRejected     func()
	Now                func() time.Time
	Path, Directory    string
	mu                 sync.Mutex
	process            *exec.Cmd
	done               chan error
	stopPending        bool
	coreRun            *core.CoreRun
	coreRunKey         *vault.Vault
	coreRunPending     bool
	runPID             int
	runToken           string
	runEpoch           string
	runRevision        int64
	runEnv             core.OSEnvironment
	current            core.RenderedConfig
	accessTail         *accessLogTail
	history            map[int64]core.RenderedConfig
	probePorts         map[string]int
	outboundHealth     map[string]scheduler.Health
}

func (a *Adapter) Name() string { return "xray" }
func (a *Adapter) DetectCapabilities(ctx context.Context) (*pb.Capabilities, error) {
	return Detect(ctx, a.ExecutablePath())
}
func (a *Adapter) Render(ctx context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	if err := state.Validate(); err != nil {
		return core.RenderedConfig{}, err
	}
	capabilities, detectErr := a.DetectCapabilities(ctx)
	at := time.Now().UTC()
	if a.Now != nil {
		at = a.Now()
	}
	scheduled, _, err := schedulingState(state)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	options, err := a.options(ctx, scheduled)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	options.OnlineStats = detectErr == nil && SupportsOnlineStats(capabilities)
	options.Access = a.AccessSink != nil || a.EnableStats
	if options.Access {
		if a.Directory == "" {
			return core.RenderedConfig{}, errors.New("Xray adapter requires a runtime directory for the access log")
		}
		options.AccessLogPath = a.accessLogPath()
	}
	if a.EnableStats {
		a.mu.Lock()
		if a.statsPort == 0 {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				a.mu.Unlock()
				return core.RenderedConfig{}, err
			}
			a.statsPort = listener.Addr().(*net.TCPAddr).Port
			listener.Close()
		}
		options.StatsPort = a.statsPort
		a.mu.Unlock()
	}
	return renderWithOptions(ctx, state, at, options)
}
func (a *Adapter) Validate(ctx context.Context, configuration core.RenderedConfig) error {
	if err := os.MkdirAll(a.Directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(a.Directory, ".validate-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(configuration.Data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(timeout, a.Path, "run", "-test", "-config", file.Name())
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("Xray native configuration validation failed")
	}
	return nil
}

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
	previous := a.current
	if err := applySwitch(ctx, previous, configuration, &result, a.stop, a.start); err != nil {
		return result, err
	}
	result.Success = true
	if a.history == nil {
		a.history = map[int64]core.RenderedConfig{}
	}
	a.history[configuration.Revision] = configuration
	result.Code = "success"
	result.Message = "configuration applied and healthy"
	return result, nil
}

// applySwitch stops the running core and starts the candidate configuration.
// Every error path that runs after a successful stop restarts the previous
// revision when one exists, or reports rollback_failed, so a failed apply never
// leaves the node without a core. stop and start are parameters so the sequence
// can be exercised without a real core process.
func applySwitch(ctx context.Context, previous, configuration core.RenderedConfig, result *core.ApplyResult, stop func() error, start func(context.Context, core.RenderedConfig) error) error {
	if err := stop(); err != nil {
		result.Code = "stop_failed"
		result.Message = "could not stop previous core"
		return err
	}
	if err := start(ctx, configuration); err != nil {
		result.Code = "start_failed"
		result.Message = "Xray startup or health check failed"
		if previous.Revision > 0 {
			if rollbackErr := start(context.Background(), previous); rollbackErr != nil {
				result.Code = "rollback_failed"
				result.Message = "Xray failed and previous revision could not restart"
				return fmt.Errorf("core startup and rollback failed")
			}
			result.RolledBack = true
		}
		return errors.New(result.Message)
	}
	return nil
}

func (a *Adapter) start(ctx context.Context, configuration core.RenderedConfig) error {
	if a.stopPending {
		return core.ErrCoreStopUnconfirmed
	}
	// Release a tail of an earlier start that never launched a core.
	a.closeAccessLogLocked()
	if err := a.lifecycleReadyLocked(); err != nil {
		return err
	}
	executable, err := exec.LookPath(a.Path)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.Directory, 0700); err != nil {
		return err
	}
	epoch, err := id.New()
	if err != nil {
		return err
	}
	configData := configuration.Data
	file, err := os.CreateTemp(a.Directory, ".running-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(configData); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// The process lifetime belongs to the Agent, never to a control connection.
	command := exec.Command(absolute, "run", "-config", file.Name())
	// The core must also die with the Agent when the supervisor only signals the
	// Agent PID (OpenRC supervise-daemon): the kernel kills the core with the
	// starting thread.
	command.SysProcAttr = core.CoreProcessAttributes()
	// Record the pending child before it exists so no start-window crash can
	// leave an orphan core without a durable marker.
	if err := a.claimCoreRunLocked(epoch, configuration.Revision); err != nil {
		return err
	}
	command.Stdout = io.Discard
	var accessSink io.Writer
	if configuration.AccessContext != nil && a.AccessSink != nil {
		accessSink = collector.NewWriter(configuration.AccessContext, a.AccessSink, a.AccessRejected)
	}
	if configuration.AccessLogPath != "" {
		// The core writes access lines into the Agent's own bounded file; the
		// tail feeds them to the line-oriented writer of this revision. A
		// configuration that renders the access log without a collector (the
		// statistics-only mode) still bounds the file.
		if accessSink == nil {
			accessSink = io.Discard
		}
		tail, err := newAccessLogTail(configuration.AccessLogPath, accessSink, accessLogLimit)
		if err != nil {
			abandonErr := a.abandonCoreRunLocked()
			return errors.Join(err, abandonErr)
		}
		a.accessTail = tail
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		a.closeAccessLogLocked()
		abandonErr := a.abandonCoreRunLocked()
		return errors.Join(err, abandonErr)
	}
	a.startedAt = time.Now()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	// Bind the exact launched child's OS identity. Failure to record an
	// authenticable identity keeps a marker that a later parent must treat as
	// unknown rather than a clean exit.
	if err := a.bindCoreRunLocked(command.Process.Pid); err != nil {
		_ = command.Process.Kill()
		if reapErr := core.ReapProcess(done, core.StopReapTimeout); reapErr != nil {
			a.process = command
			a.done = done
			a.stopPending = true
			return errors.Join(err, reapErr)
		}
		a.closeAccessLogLocked()
		confirmErr := a.confirmCoreExitLocked()
		return errors.Join(err, confirmErr)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			if err := a.abortStartLocked(command, done); err != nil {
				return err
			}
			return ctx.Err()
		case err := <-done:
			confirmErr := a.confirmCoreExitLocked()
			if err == nil {
				err = errors.New("Xray stopped during startup")
			}
			return errors.Join(err, confirmErr)
		default:
		}
		healthy := true
		for _, endpoint := range configuration.HealthEndpoints {
			connection, err := net.DialTimeout("tcp", endpoint, 100*time.Millisecond)
			if err != nil {
				healthy = false
				break
			}
			connection.Close()
		}
		if healthy {
			timer := time.NewTimer(150 * time.Millisecond)
			select {
			case err := <-done:
				timer.Stop()
				confirmErr := a.confirmCoreExitLocked()
				if err == nil {
					err = errors.New("Xray stopped during health check")
				}
				return errors.Join(err, confirmErr)
			case <-ctx.Done():
				timer.Stop()
				if err := a.abortStartLocked(command, done); err != nil {
					return err
				}
				return ctx.Err()
			case <-timer.C:
			}
			a.process = command
			a.done = done
			a.current = configuration
			a.gateDigest = ""
			a.gateRecovery = false
			a.gateFailed = false
			a.statsEpoch = epoch
			return nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			if err := a.abortStartLocked(command, done); err != nil {
				return err
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err := a.abortStartLocked(command, done); err != nil {
		return err
	}
	return errors.New("Xray health check timed out")
}

// abortStartLocked kills a child that failed to become healthy and waits, with a
// bounded budget, for its exit to be observed. An unconfirmed exit retains the
// exact process so no replacement can start for it.
func (a *Adapter) abortStartLocked(command *exec.Cmd, done chan error) error {
	_ = command.Process.Kill()
	if err := core.ReapProcess(done, core.StopReapTimeout); err != nil {
		a.process = command
		a.done = done
		a.stopPending = true
		return err
	}
	a.closeAccessLogLocked()
	confirmErr := a.confirmCoreExitLocked()
	return confirmErr
}
func (a *Adapter) stop() error {
	if a.process == nil {
		a.closeAccessLogLocked()
		return a.lifecycleReadyLocked()
	}
	select {
	case <-a.done:
		a.process = nil
		a.done = nil
		a.stopPending = false
		a.closeAccessLogLocked()
		return a.confirmCoreExitLocked()
	default:
	}
	if a.TrafficSink != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := a.sampleTrafficLocked(ctx)
		cancel()
		if err != nil {
			return errors.New("final traffic sample could not be persisted")
		}
	}
	if err := a.forceStopLocked(); err != nil {
		return err
	}
	return nil
}
func (a *Adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	err := a.stop()
	// A failed final sample prevents configuration replacement, but explicit
	// Agent shutdown must still stop its child rather than orphan the process.
	if err != nil {
		if errors.Is(err, core.ErrCoreStopUnconfirmed) {
			return err
		}
		return errors.Join(err, a.forceStopLocked())
	}
	return err
}
func (a *Adapter) forceStopLocked() error {
	if a.process == nil {
		a.closeAccessLogLocked()
		return a.lifecycleReadyLocked()
	}
	if err := a.process.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		// Killing failed for a reason other than the child already being gone, so
		// its exit is unconfirmed. Keep the original child and the original Kill
		// error, and mark the stop pending so callers refuse a replacement start
		// and leave the accounting run unresolved.
		a.stopPending = true
		return err
	}
	if err := core.ReapProcess(a.done, core.StopReapTimeout); err != nil {
		// Keep the original child and mark the stop unconfirmed so callers refuse
		// a replacement start and leave the accounting run unresolved.
		a.stopPending = true
		return err
	}
	a.process = nil
	a.done = nil
	a.stopPending = false
	a.closeAccessLogLocked()
	return a.confirmCoreExitLocked()
}

// UnconfirmedStop reports that the last forced stop could not confirm the exact
// child's exit, or that a durable marker still names an unproven child. It
// observes the durable marker rather than trusting the in-memory cache.
func (a *Adapter) UnconfirmedStop() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopPending {
		return true
	}
	observation, err := a.observeCoreRunLocked()
	return err != nil || observation == core.CoreRunBlocked
}
func (a *Adapter) Rollback(ctx context.Context, revision int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	target, exists := a.history[revision]
	if !exists {
		return errors.New("rollback revision is not loaded; use Reconciler to restore its encrypted snapshot")
	}
	if err := a.stop(); err != nil {
		return err
	}
	return a.start(ctx, target)
}
func (a *Adapter) Health(ctx context.Context) (core.HealthStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := core.HealthStatus{CheckedAt: time.Now().UTC()}
	if a.gateRecovery {
		return result, errors.New("routing recovery requires core restart")
	}
	if a.process == nil {
		if err := a.lifecycleReadyLocked(); err != nil {
			return result, err
		}
		return result, errors.New("core is not running")
	}
	select {
	case <-a.done:
		a.process = nil
		a.done = nil
		a.stopPending = false
		confirmErr := a.confirmCoreExitLocked()
		if confirmErr != nil {
			return result, errors.Join(core.ErrCoreStopUnconfirmed, confirmErr)
		}
		return result, errors.New("core exited")
	default:
	}
	for _, endpoint := range a.current.HealthEndpoints {
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", endpoint)
		if err != nil {
			return result, errors.New("core listener unavailable")
		}
		connection.Close()
	}
	result.Healthy = true
	return result, nil
}
func (a *Adapter) CollectStats(ctx context.Context) (core.StatsBatch, error) {
	counters, err := a.TrafficCounters(ctx)
	if err != nil {
		return core.StatsBatch{}, err
	}
	result := core.StatsBatch{}
	for _, counter := range counters {
		result.Upload += uint64(counter.UploadBytes)
		result.Download += uint64(counter.DownloadBytes)
	}
	return result, nil
}

var _ core.Adapter = (*Adapter)(nil)

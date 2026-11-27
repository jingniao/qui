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
	"proxycontrol/internal/agent/rulesets"
	"proxycontrol/internal/platform/id"
	access "proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"proxycontrol/pkg/contracts/traffic"
	"sync"
	"sync/atomic"
	"time"
)

type Adapter struct {
	Ports              *core.PortAllocator
	startedAt          time.Time
	connectionObserver observer.Store
	deviceGate         gate.Lease
	gateDigest         string
	gateRecovery       bool
	gateFailed         bool
	gateClearFailed    atomic.Bool
	gateProbe          gateProbeCache
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
	// RuleSets downloads the native rule set files; nil uses the defaults.
	RuleSets         *rulesets.Fetcher
	mu               sync.Mutex
	process          core.ProcessHandle
	stopPending      bool
	marker           core.RunMarker
	current          core.RenderedConfig
	accessTail       *accessLogTail
	runtimeMaterial  *core.RuntimeMaterial
	handoverPrepared bool
	detached         bool
	adopting         bool
	history          map[int64]core.RenderedConfig
	probePorts       map[string]int
	outboundHealth   map[string]scheduler.Health
}

func (a *Adapter) Name() string { return "xray" }
func (a *Adapter) DetectCapabilities(ctx context.Context) (*pb.Capabilities, error) {
	return Detect(ctx, a.ExecutablePath())
}
func (a *Adapter) Render(ctx context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	for _, in := range state.Inbounds {
		if in.Core != "" && in.Core != "xray" {
			return core.RenderedConfig{}, errors.New("inbound assigned to another core")
		}
	}
	if err := state.Validate(); err != nil {
		return core.RenderedConfig{}, err
	}
	capabilities, detectErr := a.DetectCapabilities(ctx)
	for _, inbound := range state.Inbounds {
		if inbound.Settings.HasServerSettings() {
			if detectErr != nil || capabilities.ServerSettingsContractVersion != desiredstate.SettingsContractVersion {
				return core.RenderedConfig{}, errServerSettingsUnavailable
			}
			capability := desiredstate.CoreSettingsCapability{Core: "xray", Version: capabilities.ServerSettingsContractVersion}
			for _, c := range capabilities.ServerSettingsCombinations {
				capability.Combinations = append(capability.Combinations, desiredstate.SettingsCombination{Protocol: c.Protocol, Direction: c.Direction, Security: c.Security, Transport: c.Transport, Flow: c.Flow})
			}
			if err := desiredstate.RequireInboundServerSettings("xray", capability, inbound.Protocol, inbound.Settings); err != nil {
				return core.RenderedConfig{}, errServerSettingsUnavailable
			}
		}
	}
	for _, outbound := range state.Outbounds {
		if _, required := desiredstate.OutboundSettingsCombination(outbound.Protocol, outbound.Settings); !required {
			continue
		}
		if detectErr != nil || capabilities.ServerSettingsContractVersion != desiredstate.SettingsContractVersion {
			return core.RenderedConfig{}, errServerSettingsUnavailable
		}
		capability := desiredstate.CoreSettingsCapability{Core: "xray", Version: capabilities.ServerSettingsContractVersion}
		for _, c := range capabilities.ServerSettingsCombinations {
			capability.Combinations = append(capability.Combinations, desiredstate.SettingsCombination{Protocol: c.Protocol, Direction: c.Direction, Security: c.Security, Transport: c.Transport, Flow: c.Flow})
		}
		if err := desiredstate.RequireOutboundSettings("xray", capability, outbound.Protocol, outbound.Settings); err != nil {
			return core.RenderedConfig{}, errServerSettingsUnavailable
		}
	}
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
			port, err := a.privatePortLocked(state)
			if err != nil {
				a.mu.Unlock()
				return core.RenderedConfig{}, err
			}
			a.statsPort = port
		}
		options.StatsPort = a.statsPort
		a.mu.Unlock()
	}
	if a.Ports != nil {
		options.PrivatePorts = a.Ports.Ports()
	}
	rendered, err := renderWithOptions(ctx, state, at, options)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	rendered.PrivatePorts = options.PrivatePorts
	return rendered, nil
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
	// ext: rule set files resolve against the core's own rulesets directory,
	// exactly as they do when the core runs.
	command.Env = rulesets.AssetEnvironment(os.Environ(), a.Directory)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return core.NativeValidationError(timeout, fmt.Errorf("Xray native configuration validation failed: %w", err))
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
		restoreErr := rulesets.RestoreBeforeRestart(ctx)
		if restoreErr != nil {
			result.Code = "rollback_failed"
		}
		if previous.Revision > 0 {
			if rollbackErr := start(context.Background(), previous); rollbackErr != nil {
				result.Code = "rollback_failed"
				result.Message = "Xray failed and previous revision could not restart"
				return errors.Join(err, restoreErr, rollbackErr)
			}
			result.RolledBack = true
		}
		return errors.Join(err, restoreErr)
	}
	return nil
}

func (a *Adapter) start(ctx context.Context, configuration core.RenderedConfig) error {
	if a.stopPending {
		return core.ErrCoreStopUnconfirmed
	}
	// Release a tail of an earlier start that never launched a core.
	if err := a.closeAccessLogLocked(); err != nil {
		return err
	}
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
	// Plain TCP listeners only (CVE-2026-98432 needs an MPTCP listener).
	command.Env = rulesets.AssetEnvironment(core.CoreEnvironment(), a.Directory)
	// The core must also die with the Agent when the supervisor only signals the
	// Agent PID (OpenRC supervise-daemon): the kernel kills the core with the
	// starting thread.
	command.SysProcAttr = core.CoreProcessAttributes()
	// Record the pending child before it exists so no start-window crash can
	// leave an orphan core without a durable marker.
	if err := a.claimRuntimeLocked(ctx, configuration, epoch, absolute, command); err != nil {
		return errors.Join(err, a.discardUnstartedRuntimeLocked())
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
			abandonErr := a.runMarker().Abandon()
			return errors.Join(err, abandonErr, a.discardUnstartedRuntimeLocked())
		}
		a.accessTail = tail
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		tailErr := a.closeAccessLogLocked()
		abandonErr := a.runMarker().Abandon()
		return errors.Join(err, tailErr, abandonErr, a.discardUnstartedRuntimeLocked())
	}
	a.startedAt = time.Now().UTC()
	if err := a.runMarker().Bind(command.Process.Pid); err != nil {
		// No handle owns Wait until Bind succeeds.
		_ = command.Process.Kill()
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		if reapErr := core.ReapProcess(done, core.StopReapTimeout); reapErr != nil {
			a.stopPending = true
			return errors.Join(err, reapErr)
		}
		return errors.Join(err, a.finishExitLocked())
	}
	handle, err := core.NewChildProcessHandle(command, *a.marker.Run)
	if err != nil {
		killErr := command.Process.Kill()
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		if reapErr := core.ReapProcess(done, core.StopReapTimeout); reapErr != nil {
			a.stopPending = true
			return errors.Join(err, killErr, reapErr)
		}
		return errors.Join(err, a.finishExitLocked())
	}
	a.process, a.marker.Handle = handle, handle
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, a.forceStopLocked())
		}
		exit := handle.ExitObservation()
		if exit.Exited {
			return errors.Join(errors.New("Xray stopped during startup"), exit.Err, a.finishExitLocked())
		}
		if exit.Err != nil {
			return errors.Join(exit.Err, a.forceStopLocked())
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
			case <-handle.Done():
				timer.Stop()
				return errors.Join(errors.New("Xray stopped during health check"), handle.ExitObservation().Err, a.finishExitLocked())
			case <-ctx.Done():
				timer.Stop()
				return errors.Join(ctx.Err(), a.forceStopLocked())
			case <-timer.C:
			}
			for key := range a.outboundHealth {
				if a.current.ProbeFingerprints[key] != configuration.ProbeFingerprints[key] {
					delete(a.outboundHealth, key)
				}
			}
			a.current = configuration
			a.gateDigest, a.gateRecovery, a.gateFailed = "", false, false
			a.statsEpoch = epoch
			if err := a.activateRuntimeLocked(); err != nil {
				return errors.Join(err, a.forceStopLocked())
			}
			if exit := handle.ExitObservation(); exit.Exited || exit.Err != nil {
				return errors.Join(errors.New("Xray exited or observation failed while saving runtime material"), exit.Err, a.forceStopLocked())
			}
			return nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(ctx.Err(), a.forceStopLocked())
		case <-timer.C:
		}
	}
	return errors.Join(errors.New("Xray health check timed out"), a.forceStopLocked())
}
func (a *Adapter) stop() error {
	if a.process == nil {
		return errors.Join(a.closeAccessLogLocked(), a.lifecycleReadyLocked())
	}
	if a.process.Detached() {
		return core.ErrProcessDetached
	}
	if exit := a.process.ExitObservation(); exit.Exited {
		return a.finishExitLocked()
	} else if exit.Err != nil {
		return exit.Err
	}
	if a.TrafficSink != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := a.sampleTrafficLocked(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("final traffic sample could not be persisted: %w", err)
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
	return a.closeLocked()
}

func (a *Adapter) Reset() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.closeLocked(); err != nil {
		return err
	}
	a.current = core.RenderedConfig{}
	return nil
}

func (a *Adapter) closeLocked() error {
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
	a.handoverPrepared = false
	if a.process == nil {
		return a.lifecycleReadyLocked()
	}
	ctx, cancel := context.WithTimeout(context.Background(), core.StopReapTimeout)
	defer cancel()
	if err := a.process.Stop(ctx); err != nil {
		a.stopPending = true
		return err
	}
	return a.finishExitLocked()
}

// finishExitLocked clears runtime material only after the exact run exited.
func (a *Adapter) finishExitLocked() error {
	tailErr := a.closeAccessLogLocked()
	if a.marker.Run == nil {
		return errors.Join(tailErr, core.ErrCoreStopUnconfirmed)
	}
	run := *a.marker.Run
	if err := a.recordRuntimeExitLocked(); err != nil {
		return errors.Join(tailErr, err)
	}
	if err := a.removeRuntimeLocked(run); err != nil {
		return errors.Join(tailErr, err)
	}
	if err := a.runMarker().ConfirmExit(); err != nil {
		return errors.Join(tailErr, err)
	}
	a.process = nil
	a.stopPending = false
	return tailErr
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
	observation, err := a.runMarker().Observe(a.runningChildLocked())
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
	if a.process != nil {
		if a.process.Detached() {
			return core.HealthStatus{}, core.ErrProcessDetached
		}
		exit := a.process.ExitObservation()
		return a.healthLocked(ctx, &exit)
	}
	return a.healthLocked(ctx, nil)
}

func (a *Adapter) healthLocked(ctx context.Context, exit *core.ExitObservation) (core.HealthStatus, error) {
	result := core.HealthStatus{CheckedAt: time.Now().UTC()}
	if a.gateRecovery {
		return result, errors.New("routing recovery requires core restart")
	}
	if exit == nil {
		if err := a.lifecycleReadyLocked(); err != nil {
			return result, err
		}
		return result, errors.New("core is not running")
	}
	if exit.Exited {
		cleanupErr := a.finishExitLocked()
		return result, errors.Join(errors.New("core exited"), exit.Err, cleanupErr)
	}
	if exit.Err != nil {
		return result, exit.Err
	}
	if err := a.tailErrorLocked(); err != nil {
		return result, err
	}
	for _, endpoint := range a.current.HealthEndpoints {
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", endpoint)
		if err != nil {
			// Both the sentinel and the dial error stay in the chain: the
			// supervisor restarts a core only after consecutive
			// listener-only failures, and the log keeps the real reason.
			return result, fmt.Errorf("core listener %s unavailable: %w: %w", endpoint, core.ErrListenerUnavailable, err)
		}
		connection.Close()
	}
	result.Healthy = true
	return result, nil
}

// CollectStats reports the same node total the traffic sink persists: account
// counters plus the forward inbound bytes that belong to no account.
func (a *Adapter) CollectStats(ctx context.Context) (core.StatsBatch, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.processErrorLocked(); err != nil {
		return core.StatsBatch{}, err
	}
	if a.process == nil {
		return core.StatsBatch{}, errors.New("core not running")
	}
	return a.collectStatsLocked(ctx)
}

func (a *Adapter) collectStatsLocked(ctx context.Context) (core.StatsBatch, error) {
	counters, forwards, err := readSample(ctx, a.current, a.statsEpoch, &a.counterSources)
	if err != nil {
		return core.StatsBatch{}, err
	}
	total, err := core.NodeTotal("xray", a.statsEpoch, a.current.Revision, counters, forwards)
	if err != nil {
		return core.StatsBatch{}, err
	}
	return core.StatsBatch{Upload: uint64(total.UploadBytes), Download: uint64(total.DownloadBytes)}, nil
}

var _ core.Adapter = (*Adapter)(nil)

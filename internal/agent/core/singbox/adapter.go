package singbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/scheduling"
	"proxycontrol/internal/agent/gate"
	"proxycontrol/internal/agent/observer"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ErrTelemetryUnavailable = errors.New("sing-box native account/connection telemetry has not been adapted for this build")

type Adapter struct {
	Ports                  *core.PortAllocator
	startedAt              time.Time
	connectionObserver     observer.Store
	deviceGate             gate.Lease
	sourceWriter           *collector.SingboxWriter
	gateFailed             bool
	Path, Directory        string
	NodeID                 string
	Now                    func() time.Time
	mu                     sync.Mutex
	hooks                  core.RuntimeHooks
	statsPort              int
	userStatsPort          int
	v2rayAPI               bool
	counterSources         core.CounterSources
	statsSecret            string
	statsEpoch             string
	nodeTotals             nodeTotals
	nodeUpload             uint64
	nodeDownload           uint64
	statsChecked           bool
	nativeClashAPI         bool
	nativeWireGuard        bool
	nativeWireGuardInbound bool
	probePorts             map[string]int
	outboundHealth         map[string]scheduler.Health
	process                *exec.Cmd
	done                   chan error
	stopPending            bool
	coreRun                *core.CoreRun
	coreRunKey             *vault.Vault
	coreRunPending         bool
	runPID                 int
	runToken               string
	runEpoch               string
	runRevision            int64
	runEnv                 core.OSEnvironment
	current                core.RenderedConfig
	history                map[int64]core.RenderedConfig
}

func (a *Adapter) Name() string { return "sing-box" }
func (a *Adapter) ConfigureRuntime(hooks core.RuntimeHooks) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hooks = hooks
}
func (a *Adapter) DetectCapabilities(ctx context.Context) (*pb.Capabilities, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	capabilities, err := detect(ctx, a.Path)
	if err == nil {
		a.applyUpstreamCapabilities(capabilities)
	}
	return capabilities, err
}

// applyUpstreamCapabilities records the upstream facts the renderer reads from
// this adapter. The official core reports the Clash API and the WireGuard
// protocols; every fork-only feature is gone (plan D1, D4).
func (a *Adapter) applyUpstreamCapabilities(capabilities *pb.Capabilities) {
	a.statsChecked = true
	a.nativeClashAPI = SupportsClashAPI(capabilities)
	a.v2rayAPI = SupportsV2rayAPI(capabilities)
	a.nativeWireGuard = hasWireGuardOutbound(capabilities)
	a.nativeWireGuardInbound = hasWireGuardInbound(capabilities)
}
func (a *Adapter) Render(ctx context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	for _, in := range state.Inbounds {
		if in.Core != "" && in.Core != "sing-box" {
			return core.RenderedConfig{}, errors.New("inbound assigned to another core")
		}
	}
	if err := state.Validate(); err != nil {
		return core.RenderedConfig{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	options := renderOptions{access: a.hooks.AccessSink != nil}
	if err := a.detectStatsLocked(ctx); err != nil {
		return core.RenderedConfig{}, err
	}
	options.wireGuardOutbound = a.nativeWireGuard
	options.wireGuardInbound = a.nativeWireGuardInbound
	if a.hooks.EnableStats {
		if a.v2rayAPI {
			if a.userStatsPort == 0 {
				port, err := a.privatePortLocked(state)
				if err != nil {
					return core.RenderedConfig{}, err
				}
				a.userStatsPort = port
			}
			options.userStatsPort = a.userStatsPort
		}
		if a.nativeClashAPI {
			if a.statsPort == 0 {
				port, err := a.privatePortLocked(state)
				if err != nil {
					return core.RenderedConfig{}, err
				}
				a.statsPort = port
			}
			if a.statsSecret == "" {
				secret, err := NewStatsSecret()
				if err != nil {
					return core.RenderedConfig{}, err
				}
				a.statsSecret = secret
			}
			options.statsPort = a.statsPort
			options.statsSecret = a.statsSecret
		}
	}
	at := time.Now().UTC()
	if a.Now != nil {
		at = a.Now()
	}
	scheduled, _, err := scheduling.State(state)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	probes, err := probesFor(scheduled)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	if a.probePorts == nil {
		a.probePorts = map[string]int{}
	}
	options.probePorts = map[string]int{}
	options.health = map[string]scheduler.Health{}
	options.probeFingerprints = map[string]string{}
	for id, probe := range probes {
		if a.probePorts[id] == 0 {
			port, err := a.privatePortLocked(state)
			if err != nil {
				return core.RenderedConfig{}, err
			}
			a.probePorts[id] = port
		}
		options.probePorts[id] = a.probePorts[id]
		fingerprint := probeFingerprint(scheduled, probe)
		options.probeFingerprints[id] = fingerprint
		if a.current.ProbeFingerprints[id] == fingerprint {
			if health, ok := a.outboundHealth[id]; ok {
				options.health[id] = health
			}
		}
	}
	if a.Ports != nil {
		options.privatePorts = a.Ports.Ports()
	}
	return renderWithOptions(ctx, state, at, options)
}

// privatePortLocked asks the OS for a free loopback port, excluding listeners
// reserved by this render but not yet started. Apply still validates and rolls
// back if another process takes the port between rendering and core startup.
func (a *Adapter) privatePortLocked(state desiredstate.State) (int, error) {
	if a.Ports != nil {
		return a.Ports.Allocate()
	}
	for attempt := 0; attempt < 32; attempt++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		used := port == a.statsPort || port == a.userStatsPort
		for _, entry := range state.Inbounds {
			used = used || entry.Enabled && entry.Port == port
		}
		for _, reserved := range a.probePorts {
			used = used || reserved == port
		}
		if !used {
			return port, nil
		}
	}
	return 0, errors.New("no free private statistics port")
}

func (a *Adapter) detectStatsLocked(ctx context.Context) error {
	if a.statsChecked {
		return nil
	}
	capabilities, err := detect(ctx, a.Path)
	if err == nil {
		a.applyUpstreamCapabilities(capabilities)
	}
	return err
}

// SupportsAccountTelemetry reports the detected native API capability. Builds
// without with_v2ray_api keep per-user usage unknown (D5).
func (a *Adapter) SupportsAccountTelemetry() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v2rayAPI
}

func (a *Adapter) Validate(ctx context.Context, configuration core.RenderedConfig) error {
	if len(configuration.Data) == 0 || len(configuration.Data) > 2<<20 {
		return errors.New("sing-box rendered configuration exceeds limits")
	}
	if err := os.MkdirAll(a.Directory, 0700); err != nil {
		return err
	}
	path, err := writeConfiguration(a.Directory, ".validate-", configuration.Data)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	executable, err := exec.LookPath(a.Path)
	if err != nil {
		return err
	}
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(timeout, executable, "check", "-c", path)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("sing-box native configuration validation failed")
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
		return result, err
	}
	previous := a.current
	if err := applySwitch(ctx, previous, configuration, &result, a.stopLocked, a.startLocked); err != nil {
		return result, err
	}
	if a.history == nil {
		a.history = map[int64]core.RenderedConfig{}
	}
	a.history[configuration.Revision] = configuration
	result.Success = true
	result.Code = "success"
	return result, nil
}

// applySwitch stops the running core and starts the candidate configuration.
// Every error path that runs after a successful stop restarts the previous
// revision when one exists, or reports rollback_failed, so a failed apply never
// leaves the node without a core. stop and start are parameters so the
// sequence can be exercised without a real core process.
func applySwitch(ctx context.Context, previous, configuration core.RenderedConfig, result *core.ApplyResult, stop func() error, start func(context.Context, core.RenderedConfig) error) error {
	if err := stop(); err != nil {
		result.Code = "stop_failed"
		return err
	}
	if err := start(ctx, configuration); err != nil {
		result.Code = "start_failed"
		if previous.Revision > 0 {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			rollbackErr := start(rollbackCtx, previous)
			cancel()
			if rollbackErr != nil {
				result.Code = "rollback_failed"
				return errors.New("sing-box start and rollback failed")
			}
			result.RolledBack = true
		}
		return errors.New("sing-box startup or health check failed")
	}
	return nil
}

func (a *Adapter) startLocked(ctx context.Context, configuration core.RenderedConfig) (resultErr error) {
	if a.stopPending {
		return core.ErrCoreStopUnconfirmed
	}
	if err := a.lifecycleReadyLocked(); err != nil {
		return err
	}
	executable, err := exec.LookPath(a.Path)
	if err != nil {
		return err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}
	epoch, err := id.New()
	if err != nil {
		return err
	}
	configData := configuration.Data
	path, err := writeConfiguration(a.Directory, ".running-", configData)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	// Record the pending child before it exists so no start-window crash can
	// leave an orphan core without a durable marker.
	if err := a.claimCoreRunLocked(epoch, configuration.Revision); err != nil {
		return err
	}
	command := exec.Command(executable, "run", "--disable-color", "-c", path)
	// Plain TCP listeners only (CVE-2026-98432 needs an MPTCP listener).
	command.Env = core.CoreEnvironment()
	// The core must also die with the Agent when the supervisor only signals the
	// Agent PID (OpenRC supervise-daemon): the kernel kills the core with the
	// starting thread.
	command.SysProcAttr = core.CoreProcessAttributes()
	command.Stdout = io.Discard
	if configuration.AccessContext != nil && a.hooks.AccessSink != nil {
		a.sourceWriter = collector.NewSingboxWriter(configuration.AccessContext, a.hooks.AccessSink, a.hooks.AccessRejected)
		command.Stdout = a.sourceWriter
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		abandonErr := a.abandonCoreRunLocked()
		return errors.Join(err, abandonErr)
	}
	a.startedAt = time.Now()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	accepted := false
	defer func() {
		if !accepted {
			_ = command.Process.Kill()
			if reapErr := core.ReapProcess(done, core.StopReapTimeout); reapErr != nil {
				// Retain the exact child: an unconfirmed stop must refuse any
				// replacement start.
				a.process = command
				a.done = done
				a.stopPending = true
				resultErr = errors.Join(resultErr, reapErr)
				return
			}
			confirmErr := a.confirmCoreExitLocked()
			resultErr = errors.Join(resultErr, confirmErr)
		}
	}()
	// Bind the exact launched child's OS identity. Failure keeps a marker that a
	// later parent must treat as unknown rather than a clean exit.
	if err := a.bindCoreRunLocked(command.Process.Pid); err != nil {
		return err
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			accepted = true
			confirmErr := a.confirmCoreExitLocked()
			if err == nil {
				err = errors.New("sing-box stopped during startup")
			}
			return errors.Join(err, confirmErr)
		default:
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if configurationHealthy(configuration, command.Process.Pid) {
			timer := time.NewTimer(150 * time.Millisecond)
			select {
			case err := <-done:
				timer.Stop()
				accepted = true
				confirmErr := a.confirmCoreExitLocked()
				if err == nil {
					err = errors.New("sing-box stopped during startup health")
				}
				return errors.Join(err, confirmErr)
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			a.process = command
			a.done = done
			for id := range a.outboundHealth {
				if a.current.ProbeFingerprints[id] != configuration.ProbeFingerprints[id] {
					delete(a.outboundHealth, id)
				}
			}
			a.current = configuration
			a.statsEpoch = epoch
			accepted = true
			return nil
		}
		timer := time.NewTimer(40 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errors.New("sing-box health check timed out")
}

func (a *Adapter) stopLocked() error {
	if a.process == nil {
		return a.lifecycleReadyLocked()
	}
	select {
	case <-a.done:
		a.process = nil
		a.done = nil
		a.stopPending = false
		return a.confirmCoreExitLocked()
	default:
	}
	if a.hooks.TrafficSink != nil && (a.current.StatsEndpoint != "" || a.current.UserStatsEndpoint != "") {
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

func (a *Adapter) forceStopLocked() error {
	if a.process == nil {
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
func (a *Adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.stopLocked(); err != nil {
		if errors.Is(err, core.ErrCoreStopUnconfirmed) {
			return err
		}
		return errors.Join(err, a.forceStopLocked())
	}
	return nil
}
func (a *Adapter) Rollback(ctx context.Context, revision int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	configuration, known := a.history[revision]
	if !known {
		return errors.New("sing-box successful rollback revision unavailable")
	}
	if err := a.stopLocked(); err != nil {
		return err
	}
	return a.startLocked(ctx, configuration)
}
func (a *Adapter) Health(ctx context.Context) (core.HealthStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	status := core.HealthStatus{CheckedAt: time.Now().UTC()}
	if err := ctx.Err(); err != nil {
		return status, err
	}
	if a.process == nil {
		if err := a.lifecycleReadyLocked(); err != nil {
			return status, err
		}
		return status, errors.New("sing-box core is stopped")
	}
	select {
	case <-a.done:
		a.process = nil
		a.done = nil
		a.stopPending = false
		confirmErr := a.confirmCoreExitLocked()
		if confirmErr != nil {
			return status, errors.Join(core.ErrCoreStopUnconfirmed, confirmErr)
		}
		return status, errors.New("sing-box core exited")
	default:
	}
	status.Healthy = configurationHealthy(a.current, a.process.Process.Pid)
	if !status.Healthy {
		return status, errors.New("sing-box listener health failed")
	}
	return status, nil
}
func (a *Adapter) CollectStats(context.Context) (core.StatsBatch, error) {
	return core.StatsBatch{}, ErrTelemetryUnavailable
}
func endpointsHealthy(endpoints []string) bool {
	for _, endpoint := range endpoints {
		connection, err := net.DialTimeout("tcp", endpoint, 100*time.Millisecond)
		if err != nil {
			return false
		}
		connection.Close()
	}
	return true
}

func configurationHealthy(configuration core.RenderedConfig, pid int) bool {
	if !endpointsHealthy(configuration.HealthEndpoints) {
		return false
	}
	for _, endpoint := range configuration.UDPHealthEndpoints {
		if !udpEndpointHealthy(endpoint, pid) {
			return false
		}
	}
	return true
}
func writeConfiguration(directory, prefix string, data []byte) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(directory, prefix+"*.json")
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

type boundedOutput struct{ bytes.Buffer }

func (w *boundedOutput) Write(data []byte) (int, error) {
	if w.Len()+len(data) > 16384 {
		return 0, io.ErrShortBuffer
	}
	return w.Buffer.Write(data)
}

// versionPattern matches the official `sing-box version` banner. V1 runs official
// upstream cores only (plan D1), so the version is the upstream version. A fork
// binary that still prints a fork version suffix is accepted as its base upstream
// version: the suffix is stripped, because no capability may be derived from it.
var versionPattern = regexp.MustCompile(`(?m)^sing-box version ([0-9]+\.[0-9]+\.[0-9]+)(?:-proxycontrol\.[0-9]+)?\b`)

// Detect reports the capabilities of an official upstream sing-box binary: the
// core name, the upstream version and the protocol matrix.
func Detect(ctx context.Context, path string) (*pb.Capabilities, error) {
	return detect(ctx, path)
}

func detect(ctx context.Context, path string) (*pb.Capabilities, error) {
	executable, err := exec.LookPath(path)
	if err != nil {
		return &pb.Capabilities{}, err
	}
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(timeout, executable, "version")
	output := &boundedOutput{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return &pb.Capabilities{}, err
	}
	match := versionPattern.FindStringSubmatch(output.String())
	if match == nil {
		return &pb.Capabilities{}, errors.New("configured core does not identify as sing-box")
	}
	capabilities := &pb.Capabilities{CoreName: "sing-box", CoreVersion: match[1], ForwardInboundSupported: true}
	capabilities.DeviceGateSupported = SupportsClashAPI(capabilities)
	if capabilities.DeviceGateSupported {
		capabilities.ObservationContractVersion = 2
	}
	for _, protocol := range []string{"http", "socks5", "shadowsocks", "vless", "trojan"} {
		capabilities.Protocols = append(capabilities.Protocols, &pb.ProtocolCapability{Protocol: protocol, Inbound: true, Outbound: true, Tcp: true, Udp: protocol != "http"})
	}
	tags := buildTags(output.String())
	capabilities.SingboxUserStats = tags["with_v2ray_api"]
	if tags["with_quic"] {
		for _, protocol := range []string{"hysteria2", "tuic"} {
			capabilities.Protocols = append(capabilities.Protocols, &pb.ProtocolCapability{Protocol: protocol, Inbound: true, Outbound: true, Tcp: true, Udp: true})
		}
	}
	if tags["with_wireguard"] && tags["with_gvisor"] {
		// Upstream has no WireGuard inbound that names a user per peer (plan D4),
		// so only the outbound side is reported.
		capabilities.Protocols = append(capabilities.Protocols, &pb.ProtocolCapability{Protocol: "wireguard", Inbound: false, Outbound: true, Tcp: true, Udp: true})
	}
	return capabilities, nil
}

// buildTags parses the `Tags:` line of the version banner, for example
// `Tags: with_gvisor,with_quic,with_dhcp,with_wireguard,...`.
func buildTags(output string) map[string]bool {
	tags := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "Tags:") {
			continue
		}
		for _, tag := range strings.Split(strings.TrimPrefix(line, "Tags:"), ",") {
			tags[strings.TrimSpace(tag)] = true
		}
	}
	return tags
}

// SupportsClashAPI reports whether the detected core exposes the upstream Clash
// API (`experimental.clash_api`), which is how a sing-box node reports node
// totals and connection counts. Every official upstream release is built with
// the `with_clash_api` tag (verified for 1.14.3).
func SupportsClashAPI(capabilities *pb.Capabilities) bool {
	return capabilities != nil && capabilities.CoreName == "sing-box"
}

// SupportsV2rayAPI requires the detected with_v2ray_api build tag. Versions and
// executable names cannot establish whether this optional API was compiled in.
func SupportsV2rayAPI(capabilities *pb.Capabilities) bool {
	return capabilities != nil && capabilities.CoreName == "sing-box" && capabilities.SingboxUserStats
}

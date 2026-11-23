//go:build linux

package xray

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/rulesets"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/scheduler"
)

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, f)
	err = errors.Join(readErr, f.Close())
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Native file references are part of the handover comparison, including files
// addressed indirectly through Xray's asset directory.
func (a *Adapter) runtimeDependencies(data []byte) (map[string]string, error) {
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	var walk func(any, string) error
	walk = func(value any, key string) error {
		switch v := value.(type) {
		case map[string]any:
			for k, child := range v {
				if err := walk(child, k); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range v {
				if err := walk(child, key); err != nil {
					return err
				}
			}
		case string:
			path := ""
			if key == "certificateFile" || key == "keyFile" {
				path = v
			}
			if (key == "domain" || key == "ip") && strings.HasPrefix(v, "ext:") {
				name, _, ok := strings.Cut(strings.TrimPrefix(v, "ext:"), ":")
				if !ok || name == "" {
					return errors.New("invalid Xray external asset reference")
				}
				path = filepath.Join(rulesets.Dir(a.Directory), name)
			}
			if path != "" {
				absolute, err := filepath.Abs(path)
				if err != nil {
					return err
				}
				paths[absolute] = true
			}
		}
		return nil
	}
	if err := walk(document, ""); err != nil {
		return nil, err
	}
	result := map[string]string{}
	for path := range paths {
		digest, err := fileDigest(path)
		if err != nil {
			return nil, err
		}
		result[path] = digest
	}
	return result, nil
}

func runtimeEnvironment(environment []string) map[string]string {
	result := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok && (key == "GODEBUG" || strings.HasPrefix(key, "XRAY_")) {
			result[key] = value
		}
	}
	return result
}

func (a *Adapter) claimRuntimeLocked(ctx context.Context, configuration core.RenderedConfig, epoch, executable string, command *exec.Cmd) error {
	token, err := id.New()
	if err != nil {
		return err
	}
	run := core.CoreRun{Version: core.CoreRunMarkerVersion, Kind: "core-run-marker-v1", NodeID: a.NodeID, Core: a.Name(), Epoch: epoch, Revision: configuration.Revision, Environment: core.ObserveOSEnvironment(), Process: core.ProcessIdentity{Token: token}}
	runHash, err := core.CoreRunSHA256(run)
	if err != nil {
		return err
	}
	executableHash, err := fileDigest(executable)
	if err != nil {
		return err
	}
	capabilities, err := Detect(ctx, executable)
	if err != nil {
		return err
	}
	dependencies, err := a.runtimeDependencies(configuration.Data)
	if err != nil {
		return err
	}
	material := core.RuntimeMaterial{Format: core.CoreRuntimeFormat, State: core.RuntimePrepared, Run: run, RunSHA256: runHash, RenderMetadataVersion: 1, Rendered: configuration, ConfigSHA256: core.HandoverSHA256(configuration.Data), ExecutablePath: executable, ExecutableSHA256: executableHash, ExecutableVersion: capabilities.CoreVersion, Arguments: append([]string(nil), command.Args[1:]...), Environment: runtimeEnvironment(command.Env), DependencySHA256: dependencies}
	material.ProbePorts = make(map[string]int, len(a.probePorts))
	for key, port := range a.probePorts {
		material.ProbePorts[key] = port
	}
	material.OutboundHealth = make(map[string]scheduler.Health, len(a.outboundHealth))
	for key, health := range a.outboundHealth {
		material.OutboundHealth[key] = health
		if health.CheckedAt.After(material.HealthObservedAt) {
			material.HealthObservedAt = health.CheckedAt
		}
	}
	for _, port := range configuration.PrivatePorts {
		purpose := "guard-" + strconv.Itoa(port)
		if configuration.StatsEndpoint == net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
			purpose = "stats"
		}
		for name, probePort := range a.probePorts {
			if port == probePort {
				purpose = "probe-" + name
			}
		}
		material.PrivatePorts = append(material.PrivatePorts, core.PrivatePort{Port: port, Purpose: purpose})
	}
	// Claim normally creates the key. Prepared inputs precede Claim, so establish
	// the same accounting key here without inventing any run or usage cursor.
	// An orphan runtime file also protects the original key, even without a
	// marker. Never let AccountingKeyForSave rekey that launch window.
	if _, err := os.Lstat(core.CoreRuntimePath(a.Directory)); err == nil {
		return core.ErrHandoverConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := core.AccountingKeyForSave(a.Directory); err != nil {
		return err
	}
	a.runtimeMaterial = &material
	if err := core.SaveRuntimeMaterial(a.Directory, material); err != nil {
		return err
	}
	return a.runMarker().ClaimPrepared(run)
}

func (a *Adapter) discardUnstartedRuntimeLocked() error {
	if a.runtimeMaterial == nil {
		return nil
	}
	if err := core.DiscardUnstartedRuntimeMaterial(a.Directory, a.runtimeMaterial.Run); err != nil {
		return err
	}
	a.runtimeMaterial = nil
	return nil
}

func (a *Adapter) activateRuntimeLocked() error {
	m := *a.runtimeMaterial
	m.Run = *a.marker.Run
	executableHash, err := fileDigest(filepath.Join("/proc", strconv.Itoa(m.Run.Process.PID), "exe"))
	if err != nil {
		return err
	}
	if executableHash != m.ExecutableSHA256 {
		return core.ErrHandoverConflict
	}
	dependencies, err := a.runtimeDependencies(m.Rendered.Data)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(dependencies, m.DependencySHA256) {
		return core.ErrHandoverConflict
	}
	digest, err := core.CoreRunSHA256(m.Run)
	if err != nil {
		return err
	}
	m.RunSHA256, m.State, m.StartedAt = digest, core.RuntimeActive, a.startedAt
	m.GateFree = true // A real new process loaded only the static configuration.
	if a.accessTail != nil {
		cp, err := a.accessTail.Checkpoint()
		defer a.accessTail.Resume()
		if err != nil {
			return err
		}
		m.Logs = []core.LogCheckpoint{cp}
	}
	if err := core.SaveRuntimeMaterial(a.Directory, m); err != nil {
		return err
	}
	a.runtimeMaterial = &m
	return nil
}

func (a *Adapter) persistSampleRuntimeLocked() error {
	if a.runtimeMaterial == nil {
		return errors.New("Xray active runtime material is unavailable")
	}
	snapshot, err := a.counterSources.Snapshot(a.statsEpoch)
	if err != nil {
		return err
	}
	m := *a.runtimeMaterial
	m.SeenCounterSources, m.LastSampleAt = snapshot.Seen, time.Now().UTC()
	// Routine samples only refresh memory; PrepareHandover writes the final
	// material. A changed source set is written at once so it is never lost.
	if !a.adopting && !slices.Equal(a.runtimeMaterial.SeenCounterSources, snapshot.Seen) {
		if err := core.SaveRuntimeMaterial(a.Directory, m); err != nil {
			return err
		}
	}
	a.runtimeMaterial = &m
	return nil
}

func (a *Adapter) recordRuntimeExitLocked() error {
	if a.runtimeMaterial == nil {
		return nil
	}
	m := *a.runtimeMaterial
	if a.marker.Run == nil {
		return core.ErrCoreStopUnconfirmed
	}
	// Bind failures never produce active material. Keeping the pending marker
	// and inputs is intentional: missing process identity cannot prove an exit.
	if a.marker.Run.Process.PID == 0 {
		return core.ErrCoreStopUnconfirmed
	}
	m.Run = *a.marker.Run
	var err error
	m.RunSHA256, err = core.CoreRunSHA256(m.Run)
	if err != nil {
		return err
	}
	if m.StartedAt.IsZero() {
		m.StartedAt = a.startedAt
	}
	m.State, m.ExitObservedAt, m.TailStatus = core.RuntimeExitObserved, time.Now().UTC(), "unavailable"
	slog.Warn("core exit leaves traffic tail unavailable", "event", "core_traffic_tail_unavailable", "core", "xray", "epoch", m.Run.Epoch, "last_sample_at", m.LastSampleAt)
	if err := core.SaveRuntimeMaterial(a.Directory, m); err != nil {
		return err
	}
	a.runtimeMaterial = &m
	return nil
}

func (a *Adapter) removeRuntimeLocked(run core.CoreRun) error {
	if a.runtimeMaterial == nil {
		return nil
	}
	if err := core.RemoveRuntimeMaterial(a.Directory, run); err != nil {
		return err
	}
	a.runtimeMaterial = nil
	return nil
}

func (a *Adapter) tailErrorLocked() error {
	if a.accessTail != nil {
		return a.accessTail.Err()
	}
	return nil
}

func (a *Adapter) proveGateFreeLocked(ctx context.Context, restore bool) error {
	if a.current.StatsEndpoint == "" {
		return errors.New(core.CodeGateRecoveryRequired + ": routing API unavailable")
	}
	payload, tags, err := gateRouting(a.current.Data, nil, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", core.CodeGateRecoveryRequired, err)
	}
	if restore {
		if err := a.gateCLI(ctx, payload); err != nil {
			a.gateRecovery = true
			return fmt.Errorf("%s: %w", core.CodeGateRecoveryRequired, err)
		}
	}
	if !a.verifyGateFree(ctx, tags) {
		a.gateRecovery = true
		return errors.New(core.CodeGateRecoveryRequired + ": live routing baseline was not proven")
	}
	a.deviceGate.Clear()
	a.gateDigest, a.gateRecovery, a.gateFailed = "", false, false
	a.gateProbe = gateProbeCache{}
	return nil
}

func (a *Adapter) handoverAliveLocked() error {
	if a.process == nil {
		return errors.New("Xray process handle unavailable")
	}
	if a.process.Detached() {
		return core.ErrProcessDetached
	}
	observation := a.process.ExitObservation()
	if observation.Exited {
		return core.ErrCoreExitedDuringHandover
	}
	if observation.Err != nil {
		return observation.Err
	}
	if !a.runMarker().OwnedChild(true) {
		return core.ErrCoreStopUnconfirmed
	}
	return nil
}

func (a *Adapter) PrepareHandover(ctx context.Context) (_ core.HandoverCoreReceipt, resultErr error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handoverPrepared = false
	defer func() {
		if resultErr != nil && a.accessTail != nil {
			a.accessTail.Resume()
		}
	}()
	if err := a.handoverAliveLocked(); err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	if err := a.proveGateFreeLocked(ctx, true); err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	if a.TrafficSink == nil || a.current.StatsEndpoint == "" {
		return core.HandoverCoreReceipt{}, errors.New("handover requires a persistent traffic sink and native statistics")
	}
	if err := a.sampleTrafficLocked(ctx); err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	m := *a.runtimeMaterial
	if a.accessTail != nil {
		cp, err := a.accessTail.Checkpoint()
		if err != nil {
			return core.HandoverCoreReceipt{}, err
		}
		m.Logs = []core.LogCheckpoint{cp}
	}
	m.GateFree = true
	if err := a.handoverAliveLocked(); err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	if err := core.SaveRuntimeMaterial(a.Directory, m); err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	a.runtimeMaterial = &m
	digest, err := core.RuntimeMaterialSHA256(m)
	if err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	if err := a.handoverAliveLocked(); err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	a.handoverPrepared = true
	return core.HandoverCoreReceipt{Run: m.Run, RuntimeSHA256: digest, LastSampleAt: m.LastSampleAt, Logs: m.Logs}, nil
}

func (a *Adapter) DetachForHandover() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.handoverPrepared {
		return core.ErrHandoverConflict
	}
	if err := a.handoverAliveLocked(); err != nil {
		return err
	}
	if err := a.tailErrorLocked(); err != nil {
		return err
	}
	if a.accessTail != nil {
		if err := a.accessTail.Detach(); err != nil {
			return err
		}
		a.accessTail = nil
	}
	if err := a.process.Detach(); err != nil {
		return err
	}
	a.detached = true
	a.handoverPrepared = false
	return nil
}

func (a *Adapter) Adopt(ctx context.Context, material core.RuntimeMaterial, run core.CoreRun) error {
	return a.adopt(ctx, material, run, false)
}

// AdoptForRestart installs the old accounting context before a validated
// all-core publication. It never attributes new configuration to the old run.
func (a *Adapter) AdoptForRestart(ctx context.Context, material core.RuntimeMaterial, run core.CoreRun) error {
	a.mu.Lock()
	statsPort, probes := a.statsPort, a.probePorts
	a.mu.Unlock()
	if err := a.adopt(ctx, material, run, true); err != nil {
		return err
	}
	// The old accounting configuration remains current until Apply, while the
	// next start/Render must keep the candidate's newly detected private inputs.
	a.mu.Lock()
	a.statsPort, a.probePorts = statsPort, probes
	a.mu.Unlock()
	return nil
}
func (a *Adapter) adopt(ctx context.Context, material core.RuntimeMaterial, run core.CoreRun, forRestart bool) (resultErr error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.adopting = true
	defer func() { a.adopting = false }()
	if a.process != nil || a.detached {
		return core.ErrHandoverConflict
	}
	if err := material.Validate(); err != nil {
		return err
	}
	if material.State != core.RuntimeActive || material.Run != run || run.NodeID != a.NodeID || run.Core != a.Name() || material.LastSampleAt.IsZero() {
		return core.ErrHandoverInvalid
	}
	if !material.GateFree {
		return errors.New(core.CodeGateRecoveryRequired + ": saved gate-free fact unavailable")
	}
	stored, err := core.LoadRuntimeMaterial(a.Directory)
	if err != nil {
		return err
	}
	expected, err := core.RuntimeMaterialSHA256(material)
	if err != nil {
		return err
	}
	if stored == nil {
		return core.ErrHandoverInvalid
	}
	actual, err := core.RuntimeMaterialSHA256(*stored)
	if err != nil {
		return err
	}
	if actual != expected {
		return core.ErrHandoverConflict
	}
	if !forRestart {
		executable, err := exec.LookPath(a.Path)
		if err != nil {
			return err
		}
		executable, err = filepath.Abs(executable)
		if err != nil {
			return err
		}
		digest, err := fileDigest(executable)
		if err != nil {
			return err
		}
		if executable != material.ExecutablePath || digest != material.ExecutableSHA256 {
			return core.ErrHandoverConflict
		}
		dependencies, err := a.runtimeDependencies(material.Rendered.Data)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(dependencies, material.DependencySHA256) || !reflect.DeepEqual(runtimeEnvironment(rulesets.AssetEnvironment(core.CoreEnvironment(), a.Directory)), material.Environment) {
			return core.ErrHandoverConflict
		}
	}
	handle, err := core.OpenProcessHandle(run)
	if err != nil {
		return err
	}
	// Any failed adoption releases observers only. Never kill or confirm exit.
	defer func() {
		if resultErr != nil {
			if a.accessTail != nil {
				resultErr = errors.Join(resultErr, a.accessTail.Detach())
				a.accessTail = nil
			}
			resultErr = errors.Join(resultErr, handle.Detach())
			a.process = nil
			a.marker.Clear()
			a.current = core.RenderedConfig{}
			a.runtimeMaterial = nil
		}
	}()
	liveExecutableHash, err := fileDigest(filepath.Join("/proc", strconv.Itoa(run.Process.PID), "exe"))
	if err != nil {
		return err
	}
	if liveExecutableHash != material.ExecutableSHA256 {
		return core.ErrHandoverConflict
	}
	if err := a.runMarker().Adopt(handle, run); err != nil {
		return err
	}
	a.process, a.current, a.statsEpoch, a.startedAt = handle, material.Rendered, run.Epoch, material.StartedAt
	a.runtimeMaterial = &material
	a.probePorts = make(map[string]int, len(material.ProbePorts))
	for key, port := range material.ProbePorts {
		a.probePorts[key] = port
	}
	a.outboundHealth = make(map[string]scheduler.Health, len(material.OutboundHealth))
	for key, health := range material.OutboundHealth {
		a.outboundHealth[key] = health
	}
	a.history = map[int64]core.RenderedConfig{run.Revision: material.Rendered}
	if err := a.counterSources.Restore(run.Epoch, core.CounterSourceSnapshot{Epoch: run.Epoch, Seen: material.SeenCounterSources, LastSuccess: material.LastSampleAt}); err != nil {
		return err
	}
	if material.Rendered.StatsEndpoint != "" {
		host, port, err := net.SplitHostPort(material.Rendered.StatsEndpoint)
		if err != nil {
			return err
		}
		if host != "127.0.0.1" {
			return core.ErrHandoverInvalid
		}
		a.statsPort, err = strconv.Atoi(port)
		if err != nil {
			return err
		}
	}
	if err := a.proveGateFreeLocked(ctx, false); err != nil {
		return err
	}
	if material.Rendered.AccessLogPath != "" {
		path, err := filepath.Abs(a.accessLogPath())
		if err != nil {
			return err
		}
		if len(material.Logs) != 1 || material.Logs[0].Path != path {
			return core.ErrHandoverInvalid
		}
		var sink io.Writer = io.Discard
		if material.Rendered.AccessContext != nil && a.AccessSink != nil {
			sink = collector.NewWriter(material.Rendered.AccessContext, a.AccessSink, a.AccessRejected)
		}
		tail, err := core.OpenFileTail(path, sink, accessLogLimit, &material.Logs[0])
		if err != nil {
			return err
		}
		a.accessTail = &accessLogTail{tail}
		if _, err := tail.Checkpoint(); err != nil {
			return err
		}
		defer tail.Resume()
	} else if len(material.Logs) != 0 {
		return core.ErrHandoverInvalid
	}
	if a.TrafficSink == nil || a.current.StatsEndpoint == "" {
		return errors.New("adoption requires a persistent traffic sink and native statistics")
	}
	if err := a.sampleTrafficLocked(ctx); err != nil {
		return err
	}
	for _, endpoint := range a.current.HealthEndpoints {
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", endpoint)
		if err != nil {
			return fmt.Errorf("adopted Xray listener unavailable: %w: %w", core.ErrListenerUnavailable, err)
		}
		connection.Close()
	}
	if err := a.tailErrorLocked(); err != nil {
		return err
	}
	if err := a.handoverAliveLocked(); err != nil {
		return err
	}
	if err := core.SaveRuntimeMaterial(a.Directory, *a.runtimeMaterial); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.handoverAliveLocked(); err != nil {
		return err
	}
	if a.accessTail != nil {
		return a.accessTail.CompleteAttach()
	}
	return nil
}

var _ core.HandoverRuntime = (*Adapter)(nil)

// HandoverMatches checks installed inputs before adoption changes any material.
func (a *Adapter) HandoverMatches(material core.RuntimeMaterial, candidate core.RenderedConfig) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	executable, err := exec.LookPath(a.Path)
	if err != nil {
		return false, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return false, err
	}
	hash, err := fileDigest(executable)
	if err != nil {
		return false, err
	}
	dependencies, err := a.runtimeDependencies(candidate.Data)
	if err != nil {
		return false, err
	}
	return core.HandoverRenderedEqual(material.Rendered, candidate) && executable == material.ExecutablePath && hash == material.ExecutableSHA256 && reflect.DeepEqual(dependencies, material.DependencySHA256) && reflect.DeepEqual(runtimeEnvironment(rulesets.AssetEnvironment(core.CoreEnvironment(), a.Directory)), material.Environment), nil
}
func (a *Adapter) ResumePreparedHandover() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handoverPrepared = false
	if a.accessTail != nil {
		a.accessTail.Resume()
	}
}

// ReleaseHandoverOwnership releases observers only, retaining durable evidence.
func (a *Adapter) ReleaseHandoverOwnership() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var err error
	if a.accessTail != nil {
		err = errors.Join(err, a.accessTail.Detach())
		a.accessTail = nil
	}
	if a.process != nil {
		err = errors.Join(err, a.process.Detach())
		a.process = nil
	}
	a.marker.Clear()
	a.current = core.RenderedConfig{}
	a.runtimeMaterial = nil
	a.handoverPrepared = false
	return err
}

func (a *Adapter) RestoreHandoverInputs(material core.RuntimeMaterial) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.process != nil || a.detached {
		return core.ErrHandoverConflict
	}
	if err := material.Validate(); err != nil {
		return err
	}
	if material.Run.NodeID != a.NodeID || material.Run.Core != a.Name() || material.State != core.RuntimeActive {
		return core.ErrHandoverInvalid
	}
	if material.Rendered.StatsEndpoint != "" {
		host, port, err := net.SplitHostPort(material.Rendered.StatsEndpoint)
		if err != nil {
			return err
		}
		if host != "127.0.0.1" {
			return core.ErrHandoverInvalid
		}
		a.statsPort, err = strconv.Atoi(port)
		if err != nil {
			return err
		}
	}
	a.probePorts = make(map[string]int, len(material.ProbePorts))
	for key, port := range material.ProbePorts {
		a.probePorts[key] = port
	}
	a.current = material.Rendered
	a.outboundHealth = make(map[string]scheduler.Health, len(material.OutboundHealth))
	for key, health := range material.OutboundHealth {
		a.outboundHealth[key] = health
	}
	return nil
}

// CheckPreparedHandover is the all-core detach preflight.
func (a *Adapter) CheckPreparedHandover() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.handoverPrepared {
		return core.ErrHandoverConflict
	}
	if err := a.handoverAliveLocked(); err != nil {
		return err
	}
	return a.tailErrorLocked()
}

// ReclaimDetachedForStop restores stop ownership after a failed detach commit.
func (a *Adapter) ReclaimDetachedForStop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.detached && (a.process == nil || !a.process.Detached()) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.runtimeMaterial == nil {
		return core.ErrHandoverInvalid
	}
	handle, err := core.OpenProcessHandle(a.runtimeMaterial.Run)
	if err != nil {
		return err
	}
	a.marker.Clear()
	if err := a.runMarker().Adopt(handle, a.runtimeMaterial.Run); err != nil {
		return errors.Join(err, handle.Detach())
	}
	// Keep the original prepared statistics context. Close must be able to kill
	// the owned run even when final sampling or collector drain already failed.
	a.process, a.detached, a.handoverPrepared = handle, false, false
	return nil
}

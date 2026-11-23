//go:build linux

package singbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// Native file references are part of the handover comparison, including TLS certificates and local rule sets.
func (a *Adapter) runtimeDependencies(data []byte) (map[string]string, error) {
	type tlsFiles struct {
		CertificatePath string `json:"certificate_path"`
		KeyPath         string `json:"key_path"`
	}
	type endpointFiles struct {
		TLS *tlsFiles `json:"tls"`
	}
	var doc struct {
		Inbounds  []endpointFiles `json:"inbounds"`
		Outbounds []endpointFiles `json:"outbounds"`
		Route     struct {
			RuleSet []ruleSetOption `json:"rule_set"`
		} `json:"route"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	add := func(path string) error {
		if path == "" {
			return nil
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		paths[absolute] = true
		return nil
	}
	addTLS := func(tls *tlsFiles) error {
		if tls == nil {
			return nil
		}
		return errors.Join(add(tls.CertificatePath), add(tls.KeyPath))
	}
	for _, inbound := range doc.Inbounds {
		if err := addTLS(inbound.TLS); err != nil {
			return nil, err
		}
	}
	for _, outbound := range doc.Outbounds {
		if err := addTLS(outbound.TLS); err != nil {
			return nil, err
		}
	}
	for _, ruleSet := range doc.Route.RuleSet {
		if err := add(ruleSet.Path); err != nil {
			return nil, err
		}
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
		if ok && (key == "GODEBUG" || strings.HasPrefix(key, "ENABLE_") || strings.HasPrefix(key, "SING_BOX_")) {
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
	capabilities, err := detect(ctx, executable)
	if err != nil {
		return err
	}
	dependencies, err := a.runtimeDependencies(configuration.Data)
	if err != nil {
		return err
	}
	material := core.RuntimeMaterial{Format: core.CoreRuntimeFormat, State: core.RuntimePrepared, Run: run, RunSHA256: runHash, RenderMetadataVersion: 1, Rendered: configuration, ConfigSHA256: core.HandoverSHA256(configuration.Data), ExecutablePath: executable, ExecutableSHA256: executableHash, ExecutableVersion: capabilities.CoreVersion, Arguments: append([]string(nil), command.Args[1:]...), Environment: runtimeEnvironment(command.Env), DependencySHA256: dependencies}
	material.ClashSecret = a.statsSecret
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
		} else if configuration.UserStatsEndpoint == net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
			purpose = "user-stats"
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
	if a.outputTail != nil {
		cp, err := a.outputTail.Checkpoint()
		defer a.outputTail.Resume()
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
		return errors.New("sing-box active runtime material is unavailable")
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
	slog.Warn("core exit leaves traffic tail unavailable", "event", "core_traffic_tail_unavailable", "core", "sing-box", "epoch", m.Run.Epoch, "last_sample_at", m.LastSampleAt)
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
	if a.outputTail != nil {
		return a.outputTail.Err()
	}
	return nil
}

func (a *Adapter) PrepareHandover(ctx context.Context) (_ core.HandoverCoreReceipt, resultErr error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handoverPrepared = false
	defer func() {
		if resultErr != nil && a.outputTail != nil {
			a.outputTail.Resume()
		}
	}()
	if err := a.handoverAliveLocked(); err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	a.deviceGate.Clear()
	if a.outputTail == nil {
		return core.HandoverCoreReceipt{}, errors.New("sing-box output tail unavailable")
	}
	if a.hooks.TrafficSink == nil || (a.current.StatsEndpoint == "" && a.current.UserStatsEndpoint == "") {
		return core.HandoverCoreReceipt{}, errors.New("handover requires a persistent traffic sink and native statistics")
	}
	if err := a.sampleTrafficLocked(ctx); err != nil {
		return core.HandoverCoreReceipt{}, err
	}
	m := *a.runtimeMaterial
	if a.outputTail != nil {
		cp, err := a.outputTail.Checkpoint()
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
	if a.outputTail != nil {
		if err := a.outputTail.Detach(); err != nil {
			return err
		}
		a.outputTail = nil
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
	statsPort, userPort, secret, probes := a.statsPort, a.userStatsPort, a.statsSecret, a.probePorts
	userAPI, clashAPI := a.v2rayAPI, a.nativeClashAPI
	a.mu.Unlock()
	if err := a.adopt(ctx, material, run, true); err != nil {
		return err
	}
	// The old accounting configuration remains current until Apply, while the
	// next start/Render must keep the candidate's newly detected private inputs.
	a.mu.Lock()
	a.statsPort, a.userStatsPort, a.statsSecret, a.probePorts = statsPort, userPort, secret, probes
	a.v2rayAPI, a.nativeClashAPI = userAPI, clashAPI
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
		if !reflect.DeepEqual(dependencies, material.DependencySHA256) || !reflect.DeepEqual(runtimeEnvironment(core.CoreEnvironment()), material.Environment) {
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
			if a.outputTail != nil {
				resultErr = errors.Join(resultErr, a.outputTail.Detach())
				a.outputTail = nil
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
	a.history = map[int64]core.RenderedConfig{run.Revision: material.Rendered}
	if err := a.counterSources.Restore(run.Epoch, core.CounterSourceSnapshot{Epoch: run.Epoch, Seen: material.SeenCounterSources, LastSuccess: material.LastSampleAt}); err != nil {
		return err
	}
	a.deviceGate.Clear()
	a.gateFailed = true
	a.statsSecret = material.ClashSecret
	if err := a.restorePrivateInputsLocked(material); err != nil {
		return err
	}
	if err := a.captureUnavailableConnectionsLocked(ctx); err != nil {
		return err
	}
	{
		path, err := filepath.Abs(filepath.Join(a.Directory, "output.log"))
		if err != nil {
			return err
		}
		if len(material.Logs) != 1 || material.Logs[0].Path != path {
			return core.ErrHandoverInvalid
		}
		var sink io.Writer = io.Discard
		if material.Rendered.AccessContext != nil && a.hooks.AccessSink != nil {
			a.sourceWriter = collector.NewSingboxWriter(material.Rendered.AccessContext, a.hooks.AccessSink, a.hooks.AccessRejected)
			sink = a.sourceWriter
		}
		tail, err := core.OpenFileTail(path, sink, outputLogLimit, &material.Logs[0])
		if err != nil {
			return err
		}
		a.outputTail = tail
		if _, err := tail.Checkpoint(); err != nil {
			return err
		}
	}
	if a.hooks.TrafficSink == nil || (a.current.StatsEndpoint == "" && a.current.UserStatsEndpoint == "") {
		return errors.New("adoption requires a persistent traffic sink and native statistics")
	}
	if err := a.sampleTrafficLocked(ctx); err != nil {
		return err
	}
	if a.current.StatsEndpoint != "" {
		totals, err := readClashTotals(ctx, a.current.StatsEndpoint, a.statsSecret)
		if err != nil {
			return err
		}
		a.nodeTotals = nodeTotals{upload: totals.UploadTotal, download: totals.DownloadTotal, sampled: true}
		a.nodeUpload, a.nodeDownload = totals.UploadTotal, totals.DownloadTotal
	}
	if err := configurationHealth(a.current, handle.Identity().PID); err != nil {
		return err
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
	if a.outputTail != nil {
		if err := a.outputTail.CompleteAttach(); err != nil {
			return err
		}
		a.outputTail.Resume()
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
	return core.HandoverRenderedEqual(material.Rendered, candidate) && executable == material.ExecutablePath && hash == material.ExecutableSHA256 && reflect.DeepEqual(dependencies, material.DependencySHA256) && reflect.DeepEqual(runtimeEnvironment(core.CoreEnvironment()), material.Environment), nil
}
func (a *Adapter) ResumePreparedHandover() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handoverPrepared = false
	if a.outputTail != nil {
		a.outputTail.Resume()
	}
}

// ReleaseHandoverOwnership releases observers only, retaining durable evidence.
func (a *Adapter) ReleaseHandoverOwnership() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var err error
	if a.outputTail != nil {
		err = errors.Join(err, a.outputTail.Detach())
		a.outputTail = nil
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

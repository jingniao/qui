package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
)

// Only these explicit cancellation causes may exercise update authorization.
// SIGINT, malformed lifetime input and all other cancellations keep stop semantics.
var (
	ErrTerminationRequested = errors.New("Agent termination requested")
	ErrWorkerEOF            = errors.New("managed worker lifetime EOF")
	ErrOrdinaryStop         = errors.New("ordinary Agent stop")
)

type startupHandover struct {
	request   *core.HandoverRequest
	receipt   *core.HandoverReceipt
	key       *vault.Vault
	materials map[string]core.RuntimeMaterial
	err       error
	attempt   bool
}

func handoverKey(directory string) (*vault.Vault, error) {
	key, err := os.ReadFile(filepath.Join(directory, "configuration.key"))
	if err != nil {
		return nil, err
	}
	defer clear(key)
	return vault.FromKey(key)
}

func currentAgentIdentity() (core.ProcessIdentity, error) {
	token, err := core.ObserveProcessIdentity(os.Getpid())
	return core.ProcessIdentity{PID: os.Getpid(), Token: token}, err
}

func currentAgentDigest() (string, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/exe", os.Getpid()))
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, f)
	if err := errors.Join(readErr, f.Close()); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func authorizeHandover(request core.HandoverRequest, nodeID, scope, target string, source core.ProcessIdentity) error {
	return core.AuthorizeHandoverRequest(request, core.HandoverAuthorization{NodeID: nodeID, Environment: core.ObserveOSEnvironment(), SourceAgent: source, TargetAgentSHA256: target, TransactionID: request.TransactionID, Scope: scope})
}

// inspectStartupHandover runs under the worker lifecycle lock, before inventory
// cleanup can remove disabled-core evidence. Authorization is rechecked at commit.
func inspectStartupHandover(ctx context.Context, c config.Agent, nodeID string) *startupHandover {
	h := &startupHandover{materials: map[string]core.RuntimeMaterial{}}
	if runtime.GOOS != "linux" {
		return h
	}
	var receipt *core.HandoverReceipt
	if _, err := os.Lstat(core.HandoverReceiptPath(c.DataDirectory)); err == nil {
		h.key, h.err = handoverKey(c.DataDirectory)
		if h.err == nil {
			receipt, h.err = core.LoadHandoverReceipt(c.DataDirectory, h.key)
		}
		if h.err != nil {
			h.attempt = true
			return h
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		h.attempt, h.err = true, err
		return h
	}
	h.receipt = receipt
	h.attempt = receipt != nil && (receipt.State == core.HandoverDetached || receipt.State == core.HandoverAdopting)
	runs := map[string]core.CoreRun{}
	for _, name := range core.CoreNames() {
		directory := filepath.Join(c.DataDirectory, "runtime", name)
		run, _, err := core.LoadCoreRun(directory)
		if err != nil {
			h.attempt, h.err = true, fmt.Errorf("%s marker: %w", name, err)
			return h
		}
		if run == nil {
			if _, err := os.Lstat(core.CoreRuntimePath(directory)); !errors.Is(err, os.ErrNotExist) {
				h.attempt, h.err = true, errors.Join(core.ErrHandoverInvalid, fmt.Errorf("%s runtime without marker", name), err)
				return h
			}
			continue
		}
		if run.NodeID != nodeID || run.Core != name || core.ResolveCoreRun(run, core.ObserveOSEnvironment()) == core.ProcessUnknown {
			h.attempt, h.err = true, fmt.Errorf("%s marker identity: %w", name, core.ErrCoreStopUnconfirmed)
			return h
		}
		runs[name] = *run
	}
	if !h.attempt {
		// OpenRC respawns the Agent without running stop_post. Under the new
		// worker's lifecycle lock, finish the same authenticated stop barrier
		// before restoring a revision after an ordinary crash.
		for _, name := range core.CoreNames() {
			if _, found := runs[name]; !found {
				continue
			}
			directory := filepath.Join(c.DataDirectory, "runtime", name)
			if err := core.StopRecordedCore(ctx, directory, nodeID, name); err != nil {
				h.attempt = true
				h.err = errors.Join(h.err, fmt.Errorf("%s startup stop: %w", name, err))
			}
		}
		return h
	}
	h.request, h.err = core.LoadHandoverRequest()
	if h.err != nil {
		return h
	}
	if h.err = receipt.MatchRequest(*h.request); h.err != nil {
		return h
	}
	target, err := currentAgentDigest()
	if err != nil {
		h.err = err
		return h
	}
	if h.err = authorizeHandover(*h.request, nodeID, receipt.Scope, target, receipt.SourceAgent); h.err != nil {
		return h
	}
	if core.ClassifyRecordedProcess(receipt.SourceAgent.PID, receipt.SourceAgent.Token) != core.ProcessExited {
		h.err = core.ErrHandoverConflict
		return h
	}
	if len(runs) != len(receipt.Cores) {
		h.err = core.ErrHandoverConflict
		return h
	}
	for _, item := range receipt.Cores {
		name := item.Run.Core
		if runs[name] != item.Run {
			h.err = core.ErrHandoverConflict
			return h
		}
		if core.ResolveCoreRun(&item.Run, core.ObserveOSEnvironment()) != core.ProcessAlive {
			h.err = core.ErrCoreExitedDuringHandover
			return h
		}
		material, err := core.LoadRuntimeMaterial(filepath.Join(c.DataDirectory, "runtime", name))
		if err != nil {
			h.err = fmt.Errorf("%s runtime: %w", name, err)
			return h
		}
		if material == nil {
			h.err = core.ErrHandoverInvalid
			return h
		}
		hash, err := core.RuntimeMaterialSHA256(*material)
		if err != nil {
			h.err = err
			return h
		}
		if material.Run != item.Run || hash != item.RuntimeSHA256 {
			h.err = core.ErrHandoverConflict
			return h
		}
		h.materials[name] = *material
	}
	return h
}

func (h *startupHandover) restore(ctx context.Context, c config.Agent, nodeID string, manager *reconcile.Reconciler) (err error) {
	if h.err != nil {
		if h.receipt != nil && h.request != nil && h.key != nil && h.receipt.MatchRequest(*h.request) == nil {
			receipt := *h.receipt
			receipt.State, receipt.FailureCode, receipt.Reason = core.HandoverFailed, core.HandoverFailureCode(h.err), "startup handover preflight failed"
			h.err = errors.Join(h.err, core.SaveHandoverReceipt(c.DataDirectory, h.key, *h.request, receipt))
		}
		return manager.BlockStartup(h.err)
	}
	receipt := *h.receipt
	defer func() {
		if err != nil {
			receipt.State, receipt.FailureCode, receipt.Reason = core.HandoverFailed, core.HandoverFailureCode(err), "startup adoption failed"
			err = errors.Join(err, core.SaveHandoverReceipt(c.DataDirectory, h.key, *h.request, receipt))
			err = manager.BlockStartup(err)
		}
	}()
	receipt.TargetAgent, err = currentAgentIdentity()
	if err != nil {
		return err
	}
	receipt.State = core.HandoverAdopting
	if err := core.SaveHandoverReceipt(c.DataDirectory, h.key, *h.request, receipt); err != nil {
		return err
	}
	if err := manager.RestoreStartup(ctx, h.materials); err != nil {
		return err
	}
	// Each adapter's Adopt has already committed its first native cumulative
	// account AND node sample to the original spool before returning.
	if err := authorizeHandover(*h.request, nodeID, receipt.Scope, receipt.TargetAgentSHA256, receipt.SourceAgent); err != nil {
		return err
	}
	receipt.State = core.HandoverAdopted
	return core.SaveHandoverReceipt(c.DataDirectory, h.key, *h.request, receipt)
}

func closeForHandover(ctx context.Context, c config.Agent, nodeID string, managed bool, adapter core.Runtime, drain func(context.Context) error, held bool) (bool, error) {
	cause := context.Cause(ctx)
	if runtime.GOOS != "linux" || !errors.Is(cause, ErrTerminationRequested) && !errors.Is(cause, ErrWorkerEOF) {
		return false, nil
	}
	request, err := core.LoadHandoverRequest()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("exit handover: read request: %w", err)
	}
	source, err := currentAgentIdentity()
	if err != nil {
		return false, fmt.Errorf("exit handover: own identity: %w", err)
	}
	// A managed worker owns the cores in both transactions: a bootstrap update
	// restarts the supervisor (the worker sees EOF and the same worker image is
	// the target), and a worker update replaces the worker itself. A direct
	// Agent only ever takes part in a bootstrap update.
	scope := "bootstrap"
	if managed && (request.Scope == "bootstrap" || request.Scope == "worker") {
		scope = request.Scope
	}
	if errors.Is(cause, ErrWorkerEOF) && !managed {
		return false, fmt.Errorf("%w: worker EOF reached a direct Agent", core.ErrHandoverConflict)
	}
	if err := authorizeHandover(*request, nodeID, scope, request.TargetAgentSHA256, source); err != nil {
		return false, fmt.Errorf("exit handover: authorize: %w", err)
	}
	multi, ok := adapter.(*core.MultiRuntime)
	if !ok {
		return false, core.ErrHandoverUnsupported
	}
	key, err := handoverKey(c.DataDirectory)
	if err != nil {
		return false, err
	}
	requestHash, err := core.HandoverRequestSHA256(*request)
	if err != nil {
		return false, err
	}
	receipt := core.HandoverReceipt{Version: core.HandoverProtocolVersion, Kind: "core-handover-receipt", RequestID: request.ID, RequestSHA256: requestHash, NodeID: nodeID, Environment: core.ObserveOSEnvironment(), SourceAgent: source, TargetAgentSHA256: request.TargetAgentSHA256, TransactionID: request.TransactionID, Scope: scope}
	// This context is independent of the already cancelled control lifetime.
	finish, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if held {
		err = core.ErrReleaseRecovery
	} else {
		receipt.Cores, err = multi.PrepareHandover(finish)
	}
	if err != nil {
		err = fmt.Errorf("exit handover: prepare: %w", err)
	}
	if err == nil && drain != nil {
		err = drain(finish)
	}
	if err == nil {
		err = authorizeHandover(*request, nodeID, scope, request.TargetAgentSHA256, source)
	}
	if err == nil {
		receipt.State = core.HandoverPrepared
		err = core.SaveHandoverReceipt(c.DataDirectory, key, *request, receipt)
	}
	if err == nil {
		err = multi.DetachForHandover()
	}
	if err == nil {
		receipt.State = core.HandoverDetached
		err = core.SaveHandoverReceipt(c.DataDirectory, key, *request, receipt)
	}
	if err == nil {
		return true, nil
	}
	receipt.State, receipt.FailureCode, receipt.Reason = core.HandoverFailed, core.HandoverFailureCode(err), "exit handover failed"
	saveErr := core.SaveHandoverReceipt(c.DataDirectory, key, *request, receipt)
	reclaim, reclaimCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer reclaimCancel()
	return false, errors.Join(err, saveErr, multi.CancelHandover(reclaim))
}

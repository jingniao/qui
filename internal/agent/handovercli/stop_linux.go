//go:build linux

package handovercli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/platform/config"
)

func finish(ctx context.Context, path string, c config.Agent) (resultErr error) {
	owner, err := lifecycle.Try(c.DataDirectory)
	if err != nil {
		return fmt.Errorf("worker-stop-unconfirmed: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	c.DataDirectory = owner.Directory()
	if err := confirmWorkersExited(path, c); err != nil {
		return fmt.Errorf("worker-stop-unconfirmed: %w", err)
	}
	s, err := loadLocal(c)
	if err != nil {
		return err
	}
	r, err := core.LoadHandoverRequest()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	receipt, err := core.LoadHandoverReceipt(c.DataDirectory, s.key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r != nil && r.NodeID != s.nodeID {
		return core.ErrHandoverConflict
	}
	if receipt != nil && receipt.NodeID != s.nodeID {
		return core.ErrHandoverConflict
	}
	var receiptRequest *core.HandoverRequest
	if receipt != nil && receipt.State != core.HandoverStopped {
		receiptRequest = r
		if receiptRequest == nil || receiptRequest.ID != receipt.RequestID {
			receiptRequest, err = loadRevoked(s, receipt.RequestID)
			if err != nil {
				return fmt.Errorf("receipt request evidence unavailable: %w", err)
			}
		}
		if err := requestMatches(receipt, receiptRequest, s); err != nil {
			return err
		}
	}
	if r != nil && r.BootID == core.ObserveOSEnvironment().BootID && core.ClassifyRecordedProcess(r.SourceAgent.PID, r.SourceAgent.Token) != core.ProcessExited {
		return errors.New("worker-stop-unconfirmed: source Agent exit is unproven")
	}
	if r != nil && receipt != nil && receipt.RequestID == r.ID {
		if err := requestMatches(receipt, r, s); err != nil {
			return err
		}
		if err := authorize(*r, s); err == nil {
			switch receipt.State {
			case core.HandoverDetached, core.HandoverAdopting, core.HandoverAdopted:
				return preserve(s, *receipt)
			}
		} else if !errors.Is(err, core.ErrHandoverExpired) && r.BootID == core.ObserveOSEnvironment().BootID {
			return err
		}
	}
	// Resolve every fixed directory, even for cores removed from inventory.
	for _, name := range []string{"xray", "sing-box", "gost"} {
		if err := core.StopRecordedCore(ctx, runtimeDirectory(s.config.DataDirectory, name), s.nodeID, name); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("%s: %w", name, err))
		}
	}
	if resultErr != nil {
		return resultErr
	}
	if receipt != nil && receipt.State != core.HandoverStopped {
		next := *receipt
		next.State, next.FailureCode, next.Reason = core.HandoverStopped, "", ""
		if err := core.SaveHandoverReceipt(c.DataDirectory, s.key, *receiptRequest, next); err != nil {
			return err
		}
	}
	return nil
}

func preserve(s *localState, receipt core.HandoverReceipt) error {
	expected := map[string]core.HandoverCoreReceipt{}
	for _, item := range receipt.Cores {
		expected[item.Run.Core] = item
	}
	for _, name := range []string{"xray", "sing-box", "gost"} {
		directory := runtimeDirectory(s.config.DataDirectory, name)
		run, _, err := core.LoadCoreRun(directory)
		if err != nil {
			return err
		}
		item, found := expected[name]
		if run == nil {
			if found {
				return conflict("receipt core marker is absent")
			}
			if _, err := os.Lstat(core.CoreRuntimePath(directory)); !errors.Is(err, os.ErrNotExist) {
				return errors.Join(core.ErrHandoverConflict, err)
			}
			continue
		}
		if !found || *run != item.Run {
			return conflict("receipt core set differs from markers")
		}
		material, err := core.LoadRuntimeMaterial(directory)
		if err != nil {
			return err
		}
		digest, err := core.RuntimeMaterialSHA256(*material)
		if err != nil {
			return err
		}
		if digest != item.RuntimeSHA256 {
			return conflict("runtime material differs from detached receipt")
		}
		handle, err := core.OpenProcessHandle(*run)
		if err != nil {
			return err
		}
		exit := handle.ExitObservation()
		detachErr := handle.Detach()
		if exit.Exited || exit.Err != nil {
			return errors.Join(core.ErrCoreExitedDuringHandover, exit.Err, detachErr)
		}
		if detachErr != nil {
			return detachErr
		}
	}
	return nil
}

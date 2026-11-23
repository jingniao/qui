package core

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"
)

// StopRecordedCore authenticates and stops a previous worker's core. The caller
// must hold the worker lifecycle lock and rule out an authorized handover first.
func StopRecordedCore(ctx context.Context, directory, nodeID, name string) error {
	run, key, err := LoadCoreRun(directory)
	if err != nil {
		return err
	}
	if run == nil {
		if _, err := os.Lstat(CoreRuntimePath(directory)); errors.Is(err, os.ErrNotExist) {
			return nil
		} else {
			return errors.Join(ErrHandoverConflict, errors.New("runtime material without core marker"), err)
		}
	}
	if run.NodeID != nodeID || run.Core != name {
		return errors.Join(ErrHandoverConflict, errors.New("core marker node or directory identity differs"))
	}
	state := ResolveCoreRun(run, ObserveOSEnvironment())
	if state != ProcessAlive && state != ProcessExited {
		return ErrCoreStopUnconfirmed
	}
	// Authenticate any companion before killing or deleting anything. Legacy
	// runs without a companion still retain their authenticated core-run marker.
	var material *RuntimeMaterial
	if _, err := os.Lstat(CoreRuntimePath(directory)); err == nil {
		material, err = LoadRuntimeMaterialForStop(directory)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	marker := &RunMarker{Directory: directory, NodeID: nodeID, Core: name, Run: run, Key: key}
	if state == ProcessAlive {
		handle, err := OpenProcessHandle(*run)
		if err != nil {
			return err
		}
		stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = handle.Stop(stopCtx)
		cancel()
		if err != nil {
			return errors.Join(err, handle.Detach())
		}
		if !handle.ExitObservation().Exited {
			return errors.Join(ErrCoreStopUnconfirmed, handle.Detach())
		}
		marker.Handle = handle
	}
	if material != nil && material.State == RuntimeActive {
		material.State, material.ExitObservedAt, material.TailStatus = RuntimeExitObserved, time.Now().UTC(), "unavailable"
		if err := SaveRuntimeMaterial(directory, *material); err != nil {
			if marker.Handle != nil {
				err = errors.Join(err, marker.Handle.Detach())
			}
			return err
		}
	}
	// D2: this helper has no live adapter/sink. Record the gap, never invent a
	// final cumulative sample or reuse the last sample as a complete ending.
	slog.Warn("core traffic tail unavailable", "code", "core_traffic_tail_unavailable", "node_id", nodeID, "core", name, "epoch", run.Epoch, "revision", run.Revision, "pid", run.Process.PID, "token", run.Process.Token, "tail_status", "unavailable")
	// Remove the authenticated companion first so a crash cannot strand a
	// companion whose marker is already gone and cannot be authenticated again.
	if material != nil {
		if err := RemoveRuntimeMaterial(directory, *run); err != nil {
			if marker.Handle != nil {
				err = errors.Join(err, marker.Handle.Detach())
			}
			return err
		}
	}
	return marker.ConfirmExit()
}

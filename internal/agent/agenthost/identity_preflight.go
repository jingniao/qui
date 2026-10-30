package agenthost

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/agentstorage"
)

// CheckCoreAccountingActivation proves, before the durable core-run marker ABI
// floor is raised, that every Agent image that could be restored (the original
// local baseline and the current/previous signed installations) can read the new
// write, and that the running program can read the floor it would persist.
// Raising the floor without this proof would leave an older fallback image unable
// to start after a control-only recovery. An independent Agent has no retained
// image and is checked against its original authenticated storage floor.
func CheckCoreAccountingActivation(ctx context.Context, directory, nodeID, version string) error {
	return checkFormatActivation(ctx, directory, nodeID, version, false)
}
func CheckMultiCoreActivation(ctx context.Context, directory, nodeID, version string) error {
	return checkFormatActivation(ctx, directory, nodeID, version, true)
}
func checkFormatActivation(ctx context.Context, directory, nodeID, version string, multi bool) error {
	active, err := Active(directory)
	if err != nil {
		return err
	}
	running := agentstorage.Current(version)
	if running.Validate() != nil {
		return ErrState
	}
	if !active {
		required, present, err := storage.Check(directory, nodeID, running)
		if err != nil || !present {
			return ErrState
		}
		raising, err := agentstorage.WithRequiredWriter(running, required)
		if err != nil {
			return ErrState
		}
		raising.Formats.CoreAccounting.Write = agentstorage.CoreAccountingFormat
		if multi {
			raising.Formats.Configuration.Write = 4
			raising.Formats.Spool.Write = 6
			raising.Formats.DesiredState.Write = 3
			raising.Formats.ReleaseHold.Write = 3
		}
		baseline := required
		if multi {
			baseline = running
		}
		if raising.Validate() != nil || agentstorage.CheckReplacement(baseline, raising) != nil {
			return ErrState
		}
		return nil
	}
	key, err := readOriginal(filepath.Join(directory, "configuration.key"), 32)
	if err != nil {
		return err
	}
	defer clear(key)
	r, err := readRecord(filepath.Join(directory, "identity", markerName), key, nodeID)
	if err != nil {
		return err
	}
	if r.Jobs {
		jobs, err := agentjobs.InspectRecoveryJobs(jobsOptions(directory, nodeID, key))
		if err != nil {
			return err
		}
		if !jobs.Present || jobs.PendingJobs > 0 || jobs.NeedsRecovery {
			return ErrState
		}
	}
	required, _, err := storage.Check(directory, nodeID, running)
	if err != nil {
		return err
	}
	required, err = agentstorage.WithRequiredWriter(running, required)
	if err != nil {
		return err
	}
	required.Formats.CoreAccounting.Write = agentstorage.CoreAccountingFormat
	if multi {
		required.Formats.Configuration.Write = 4
		required.Formats.Spool.Write = 6
		required.Formats.DesiredState.Write = 3
		required.Formats.ReleaseHold.Write = 3
	}
	if required.Validate() != nil {
		return ErrState
	}
	return updater.CheckIdentityActivation(ctx, updater.HistoryOptions{Directory: filepath.Join(directory, "agent-release-history"), Checkpoint: filepath.Join(directory, "identity", "agent-release-checkpoint.enc"), NodeID: nodeID, Key: key}, filepath.Join(directory, "releases", "agent-installed"), r.Baseline, required)
}

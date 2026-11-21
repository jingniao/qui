package agenthost

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/agent/configuration"
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
	return checkFormatActivation(ctx, directory, nodeID, version, false, false, false)
}

// raiseWrites sets the write formats activation would persist: core accounting
// always, and the multi-core configuration, spool, desired-state and release
// hold formats when multi is set.
func raiseWrites(c *agentstorage.Contract, multi, trafficDelivery bool) {
	if trafficDelivery {
		c.Formats.Spool.Write = agentstorage.TrafficDeliveryFormat
		return
	}
	c.Formats.CoreAccounting.Write = agentstorage.CoreAccountingFormat
	if multi {
		c.Formats.Configuration.Write = max(c.Formats.Configuration.Write, 4)
		c.Formats.Spool.Write = max(c.Formats.Spool.Write, 6)
		c.Formats.DesiredState.Write = max(c.Formats.DesiredState.Write, 3)
		c.Formats.ReleaseHold.Write = max(c.Formats.ReleaseHold.Write, 3)
	}
}

// raiseFormats is the supervisor's activation, done before any Worker starts and
// with the release history it already holds open (the Worker can never take that
// history's locks). Retained images and idle jobs are proven first; when the proof
// fails nothing is raised. required is the stored floor and reader the running
// supervisor's contract.
func raiseFormats(ctx context.Context, directory, nodeID string, reader, required agentstorage.Contract, history *updater.History, images string, baseline updater.Image, jobsIdle bool) error {
	if required.Formats.CoreAccounting.Write >= agentstorage.CoreAccountingFormat && required.Formats.Configuration.Write >= 4 {
		return nil
	}
	if !jobsIdle {
		return ErrState
	}
	target, err := agentstorage.WithRequiredWriter(reader, required)
	if err != nil {
		return ErrState
	}
	raiseWrites(&target, true, false)
	if target.Validate() != nil {
		return ErrState
	}
	if err := history.CheckActivation(ctx, images, baseline, target); err != nil {
		return err
	}
	if _, err := storage.ActivateCoreAccountingFormat(directory, nodeID, reader); err != nil {
		return err
	}
	_, err = storage.ActivateCoreInventoryFormats(directory, nodeID, reader)
	return err
}

func CheckMultiCoreActivation(ctx context.Context, directory, nodeID, version string) error {
	return checkFormatActivation(ctx, directory, nodeID, version, true, false, false)
}
func checkFormatActivation(ctx context.Context, directory, nodeID, version string, multi, trafficDelivery, coreOptional bool) error {
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
		raiseWrites(&raising, multi, trafficDelivery)
		if coreOptional {
			raising.Formats.Configuration.Write = 6
		}
		baseline := required
		if multi || coreOptional {
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
	raiseWrites(&required, multi, trafficDelivery)
	if coreOptional {
		required.Formats.Configuration.Write = 6
	}
	if required.Validate() != nil {
		return ErrState
	}
	return updater.CheckIdentityActivation(ctx, updater.HistoryOptions{Directory: filepath.Join(directory, "agent-release-history"), Checkpoint: filepath.Join(directory, "identity", "agent-release-checkpoint.enc"), NodeID: nodeID, Key: key}, filepath.Join(directory, "releases", "agent-installed"), r.Baseline, required)
}

// CheckCoreOptionalActivation proves the irreversible format 6 raise. Managed
// workers delegate the proof and write to the supervisor holding history locks.
func CheckCoreOptionalActivation(ctx context.Context, directory, nodeID, version string) error {
	active, err := Active(directory)
	if err != nil {
		return err
	}
	if active {
		status, err := callLocal(ctx, directory, localRequest{Operation: "activate-core-optional"})
		if err != nil {
			return err
		}
		if status.NodeID != nodeID {
			return ErrState
		}
		return nil
	}
	return checkFormatActivation(ctx, directory, nodeID, version, true, false, true)
}

func raiseCoreOptionalFormat(ctx context.Context, directory, nodeID string, reader, required agentstorage.Contract, history *updater.History, images string, baseline updater.Image, jobsIdle bool) error {
	if required.Formats.Configuration.Write >= 6 {
		return nil
	}
	if !jobsIdle {
		return ErrState
	}
	target, err := agentstorage.WithRequiredWriter(reader, required)
	if err != nil {
		return err
	}
	raiseWrites(&target, true, false)
	target.Formats.Configuration.Write = 6
	if err := history.CheckActivation(ctx, images, baseline, target); err != nil {
		return fmt.Errorf("fallback_incompatible: %w", err)
	}
	_, err = storage.ActivateCoreOptionalFormat(directory, nodeID, reader)
	return err
}

func coreOptionalRequired(configPath, directory, nodeID string) (bool, error) {
	c, err := configuration.LoadAgent(configPath)
	if err != nil {
		return false, err
	}
	inventory, err := configuration.ReadInventory(directory, nodeID)
	if err != nil {
		return false, err
	}
	cores, err := inventory.Merge(c)
	if err != nil {
		return false, err
	}
	return configuration.PrimaryCore(cores).Name == "", nil
}

// The supervisor event loop serializes this fixed operation with installation.
// Hold the jobs store too, so another caller cannot queue a change during proof.
func (r *jobRunner) activateCoreOptional() (resultErr error) {
	if r.bootstrapStore != nil {
		return ErrState
	}
	record, err := readRecord(filepath.Join(r.directory, "identity", markerName), r.key, r.nodeID)
	if err != nil {
		return err
	}
	if record.Jobs {
		store, err := agentjobs.Open(jobsOptions(r.directory, r.nodeID, r.key))
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
		if store.Pending() != nil || store.NeedsRecovery() {
			return ErrState
		}
	}
	if err := r.refreshRequired(); err != nil {
		return err
	}
	if err := raiseCoreOptionalFormat(r.ctx, r.directory, r.nodeID, r.reader, r.required, r.history, filepath.Join(r.directory, "releases", "agent-installed"), record.Baseline, true); err != nil {
		return err
	}
	return r.refreshRequired()
}

func CheckTrafficDeliveryActivation(ctx context.Context, directory, nodeID, version string) error {
	return checkFormatActivation(ctx, directory, nodeID, version, false, true, false)
}

func raiseTrafficDeliveryFormat(ctx context.Context, directory, nodeID string, reader, required agentstorage.Contract, history *updater.History, images string, baseline updater.Image, jobsIdle bool) error {
	if required.Formats.Spool.Write >= agentstorage.TrafficDeliveryFormat {
		return nil
	}
	if !jobsIdle {
		return ErrState
	}
	target, err := agentstorage.WithRequiredWriter(reader, required)
	if err != nil {
		return err
	}
	target.Formats.Spool.Write = agentstorage.TrafficDeliveryFormat
	if target.Validate() != nil {
		return ErrState
	}
	if err = history.CheckActivation(ctx, images, baseline, target); err != nil {
		return err
	}
	_, err = storage.ActivateTrafficDeliveryFormat(directory, nodeID, reader)
	return err
}

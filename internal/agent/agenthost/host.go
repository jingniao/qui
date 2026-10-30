package agenthost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/configuration"
	"time"

	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/agent/agentprocess"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/agentstorage"
)

// Run holds supervisor ownership for its lifetime. Initialization is explicit;
// subsequent ordinary starts automatically honor the authenticated history.
func Run(ctx context.Context, configPath, version string, initialize bool, logger *slog.Logger) (resultErr error) {
	if configPath == "" {
		return errors.New("supervised Agent requires an explicit configuration file")
	}
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	c, err := configuration.LoadAgent(configPath)
	if err != nil {
		return err
	}
	if logger == nil {
		logger = slog.Default()
	}
	owner, err := lifecycle.TrySupervisor(c.DataDirectory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	c.DataDirectory = owner.Directory()
	workerOwner, err := lifecycle.Try(c.DataDirectory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, workerOwner.Close()) }()
	node, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil {
		return err
	}
	required, adopted, err := storage.Check(c.DataDirectory, node.NodeID, agentstorage.Current(version))
	if err != nil {
		return err
	}
	if !adopted {
		return errors.New("run a compatible enrolled Agent before enabling supervision")
	}
	key, err := readOriginal(filepath.Join(c.DataDirectory, "configuration.key"), 32)
	if err != nil {
		return err
	}
	defer clear(key)
	revision, err := reconcile.InspectCurrent(c.DataDirectory, node.NodeID, key)
	if err != nil {
		return err
	}
	active, err := Active(c.DataDirectory)
	if err != nil {
		return err
	}
	images := filepath.Join(c.DataDirectory, "releases", "agent-installed")
	marker := filepath.Join(c.DataDirectory, "identity", markerName)
	options := updater.HistoryOptions{Directory: filepath.Join(c.DataDirectory, "agent-release-history"), Checkpoint: filepath.Join(c.DataDirectory, "identity", "agent-release-checkpoint.enc"), NodeID: node.NodeID, Key: key}
	var history *updater.History
	if initialize {
		if active {
			return errors.New("Agent supervision already exists or is incomplete; original state must be recovered")
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		image, err := updater.InspectImage(ctx, "proxy-agent", executable, version)
		if err != nil {
			return err
		}
		if _, err := updater.MaterializeImage(ctx, images, executable, "proxy-agent", image); err != nil {
			return err
		}
		probe, err := updater.ProbeAgentImage(ctx, images, image, required)
		if err != nil || probe.Contract.ManagedProtocol != 1 {
			return errors.Join(ErrState, err)
		}
		if err := createRecord(marker, key, record{Kind: "agent-supervision-v1", NodeID: node.NodeID, Baseline: image, Jobs: true}); err != nil {
			return err
		}
		if err := agentjobs.Initialize(jobsOptions(c.DataDirectory, node.NodeID, key)); err != nil {
			return err
		}
		history, err = updater.CreateHistory(options)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, history.Close()) }()
		if err := history.BootstrapImage(ctx, "proxy-agent", image); err != nil {
			return err
		}
	} else {
		if !active {
			return errors.New("enable Agent supervision with --initialize-agent-history first")
		}
		history, err = updater.OpenHistory(options)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, history.Close()) }()
	}
	r, err := readRecord(marker, key, node.NodeID)
	if err != nil {
		return err
	}
	needsRecovery := false
	if r.Jobs {
		store, err := agentjobs.OpenForStartup(ctx, jobsOptions(c.DataDirectory, node.NodeID, key))
		if err != nil {
			return err
		}
		needsRecovery = store.NeedsRecovery()
		if pending := store.Pending(); pending != nil && pending.Started {
			// A start record without an installation event cannot prove that
			// the old Worker drained. Never reopen data access or retry install.
			_, installed, lookupErr := history.InstallationRequest(pending.Request.ID)
			if lookupErr != nil {
				return errors.Join(lookupErr, store.Close())
			}
			needsRecovery = needsRecovery || pending.Request.Operation == "recover" || !installed
		}
		if err := store.Close(); err != nil {
			return err
		}
	}
	state, found, err := history.Installation("proxy-agent")
	if err != nil || !found {
		return errors.Join(ErrState, err)
	}
	if state.Generation == 0 && state.Current != r.Baseline {
		return ErrState
	}
	probe, err := updater.ProbeAgentImage(ctx, images, state.Current, required)
	if err != nil || probe.Contract.ManagedProtocol != 1 {
		return errors.Join(ErrState, err)
	}
	if err := workerOwner.Close(); err != nil {
		return err
	}
	var process *agentprocess.Process
	startupCleanup := true
	defer func() {
		if startupCleanup && process != nil {
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			resultErr = errors.Join(resultErr, process.Stop(stopCtx))
		}
	}()
	baseline, baselinePending, err := history.PendingBaselineRecovery("proxy-agent")
	if err != nil {
		return err
	}
	if baselinePending || state.Phase == "failed_closed" || needsRecovery {
		if !r.Jobs || baselinePending && baseline.Request.Revision != revision || state.Phase == "failed_closed" && state.Revision != revision {
			return ErrState
		}
		process, err = agentprocess.StartControlOnly(ctx, images, state.Current, configPath, node.NodeID, revision, required, os.Stderr)
		if err != nil {
			return err
		}
	} else if state.Phase == "switching" || state.Phase == "rolling_back" {
		if revision != state.Revision {
			return errors.New("pending Agent installation revision differs from retained state")
		}
		driver, err := agentprocess.NewDriver(ctx, images, configPath, node.NodeID, revision, state.Current, nil, required, os.Stderr)
		if err != nil {
			return err
		}
		installer := &updater.Installer{History: history, Directory: images}
		if r.Jobs && probe.Contract.AgentUpdateProtocol == 1 {
			driver.EnableAgentUpdates()
			if probe.Contract.AgentRecoveryProtocol == 1 {
				driver.EnableAgentRecovery()
			}
		}
		if _, err := installer.Recover(ctx, driver); err != nil {
			process = driver.Process()
			if process != nil {
				return errors.Join(err, agentprocess.ErrStopUnconfirmed)
			}
			retained, found, readErr := history.Installation("proxy-agent")
			if readErr != nil || !found || retained.Phase != "failed_closed" || retained.Current != state.Current || !r.Jobs {
				return errors.Join(err, readErr)
			}
			process, readErr = agentprocess.StartControlOnly(ctx, images, retained.Current, configPath, node.NodeID, revision, required, os.Stderr)
			if readErr != nil {
				return errors.Join(err, readErr)
			}
		} else {
			process = driver.Process()
		}
	} else {
		process, err = agentprocess.StartRetained(ctx, images, state.Current, configPath, node.NodeID, revision, os.Stderr, r.Jobs && probe.Contract.AgentUpdateProtocol == 1, r.Jobs && probe.Contract.AgentRecoveryProtocol == 1)
		if err != nil {
			return err
		}
	}
	if process == nil {
		return ErrState
	}
	runner := &jobRunner{ctx: ctx, configPath: configPath, directory: c.DataDirectory, nodeID: node.NodeID, key: key, required: required, reader: agentstorage.Current(version), history: history, process: process}
	startupCleanup = false
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if runner.process != nil {
			resultErr = errors.Join(resultErr, runner.process.Stop(stopCtx))
		}
	}()
	logger.Info("supervised Agent runtime started", "agent_version", state.Current.Version, "degraded", process.Degraded())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastJobError := ""
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-runner.process.Done():
			if ctx.Err() != nil {
				return nil
			}
			return errors.Join(fmt.Errorf("supervised Agent runtime exited unexpectedly"), runner.process.Wait())
		case <-ticker.C:
			if r.Jobs {
				if err := runner.poll(); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					if lastJobError != err.Error() {
						logger.Error("Agent update remains unconfirmed", "error", err)
					}
					lastJobError = err.Error()
				} else {
					lastJobError = ""
				}
				if runner.process == nil {
					return errors.New("Agent update left no confirmed runtime")
				}
			}
		}
	}
}

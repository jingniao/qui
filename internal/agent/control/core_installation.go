package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/config"
)

// coreInstallation is the authenticated core installation found at startup.
// A nil value means the node has never installed a core image and runs the
// operator-configured executable.
type coreInstallation struct {
	history   *updater.History
	installer *updater.Installer
	state     *updater.Installation
	path      string
}

func (i *coreInstallation) Close() {
	if i != nil && i.history != nil {
		i.history.Close()
	}
}

// coreComponent is the managed core of this node.
func coreComponent(c config.Agent) string {
	if c.CoreName == "" {
		return "xray"
	}
	return c.CoreName
}

func coreInstallationPending(state updater.Installation) bool {
	return state.Phase == "switching" || state.Phase == "rolling_back" || state.Phase == "failed_closed"
}

// openCoreHistory opens the authenticated core installation history. It
// reports an error when only one of the two history anchors exists, because a
// partially initialized history must not be replaced silently.
func openCoreHistory(c config.Agent, nodeID string) (*updater.History, error) {
	directory := filepath.Join(c.DataDirectory, "release-history")
	checkpoint := filepath.Join(c.DataDirectory, "identity", "release-checkpoint.enc")
	_, directoryErr := os.Lstat(directory)
	_, checkpointErr := os.Lstat(checkpoint)
	if errors.Is(directoryErr, os.ErrNotExist) && errors.Is(checkpointErr, os.ErrNotExist) {
		return nil, updater.ErrHistory
	}
	key, err := os.ReadFile(filepath.Join(c.DataDirectory, "configuration.key"))
	if err != nil {
		return nil, err
	}
	defer clear(key)
	return updater.OpenHistory(updater.HistoryOptions{Directory: directory, Checkpoint: checkpoint, NodeID: nodeID, Key: key})
}

// openCoreInstallation loads the executable the node must run and the state
// used to finish an interrupted switch. It returns nil when the node has never
// recorded a core installation.
func openCoreInstallation(ctx context.Context, c config.Agent, nodeID string) (*coreInstallation, error) {
	directory := filepath.Join(c.DataDirectory, "release-history")
	checkpoint := filepath.Join(c.DataDirectory, "identity", "release-checkpoint.enc")
	_, directoryErr := os.Lstat(directory)
	_, checkpointErr := os.Lstat(checkpoint)
	if errors.Is(directoryErr, os.ErrNotExist) && errors.Is(checkpointErr, os.ErrNotExist) {
		return nil, nil
	}
	history, err := openCoreHistory(c, nodeID)
	if err != nil {
		return nil, err
	}
	component := coreComponent(c)
	installer := &updater.Installer{History: history, Directory: filepath.Join(c.DataDirectory, "releases", "installed")}
	state, exists, err := history.Installation(component)
	if err != nil {
		history.Close()
		return nil, err
	}
	if !exists {
		return &coreInstallation{history: history, installer: installer, path: c.CorePath}, nil
	}
	path, err := updater.ResolveImage(ctx, installer.Directory, component, state.Current)
	if err != nil {
		history.Close()
		return nil, err
	}
	return &coreInstallation{history: history, installer: installer, state: &state, path: path}, nil
}

// repairCoreInstallation rolls an interrupted core switch back to the retained
// image before the supervisor starts, and removes staging left by an
// interrupted download.
func repairCoreInstallation(ctx context.Context, c config.Agent, installation *coreInstallation, manager *reconcile.Reconciler) (resultErr error) {
	staging := filepath.Join(c.DataDirectory, "releases", "core-staging")
	if err := os.RemoveAll(staging); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if manager == nil {
		return nil
	}
	pending := installation != nil && installation.state != nil && coreInstallationPending(*installation.state)
	if !pending && !manager.CoreUpdateHeld() {
		return nil
	}
	path := c.CorePath
	if installation != nil && installation.path != "" {
		path = installation.path
	}
	if err := manager.RepairCoreRollback(ctx, path); err != nil {
		return err
	}
	if !pending {
		return nil
	}
	if _, err := installation.history.TransitionInstallation(ctx, installation.state.RequestID, "rolling_back"); err != nil {
		return err
	}
	_, err := installation.history.TransitionInstallation(ctx, installation.state.RequestID, "rolled_back")
	return err
}
